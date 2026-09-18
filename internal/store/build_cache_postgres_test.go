package store_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"xprem/internal/bucket"
	"xprem/internal/database"
	"xprem/internal/database/postgres/pgdb"
	"xprem/internal/handlers"
	"xprem/internal/services"
	"xprem/internal/store"
	"xprem/internal/types"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestBuildCachePublicationAndReplacement(t *testing.T) {
	f := setupBuildStore(t)
	ctx := context.Background()
	repo := store.NewPostgresBuildCacheStore(&database.Engine{Queries: pgdb.New(f.pool), DB: f.pool})
	storage := &bucket.LocalBucket{BasePath: t.TempDir()}
	service := services.NewBuildCacheService(repo, storage)
	input := services.BuildCacheInput{Namespace: types.BuildCacheCcache, Key: "manifest", Size: 5, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("first")))}
	upload, err := service.Reserve(ctx, f.app, f.identifier, input)
	require.NoError(t, err)
	_, err = service.Find(ctx, f.app, f.identifier, input.Namespace, input.Key)
	require.Error(t, err, "unverified uploads are never cache hits")
	require.NoError(t, service.UploadLocal(ctx, f.app, f.identifier, upload.Object.ID, strings.NewReader("first")))
	_, err = service.Complete(ctx, f.app, f.identifier, upload.Object.ID)
	require.NoError(t, err)
	_, err = service.Complete(ctx, f.app, f.identifier, upload.Object.ID)
	require.NoError(t, err, "completion is retryable")
	duplicate, err := service.Reserve(ctx, f.app, f.identifier, input)
	require.NoError(t, err)
	require.True(t, duplicate.Cached)
	require.Equal(t, upload.Object.ID, duplicate.Object.ID)
	require.Error(t, service.UploadLocal(ctx, f.app, f.identifier, upload.Object.ID, strings.NewReader("evil!")))
	_, err = service.Get(ctx, uuid.NewString(), f.identifier, upload.Object.ID)
	require.Error(t, err)
	_, err = service.Find(ctx, f.app, uuid.NewString(), input.Namespace, input.Key)
	require.Error(t, err)

	input.SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte("later")))
	newUpload, err := service.Reserve(ctx, f.app, f.identifier, input)
	require.NoError(t, err)
	require.NoError(t, service.UploadLocal(ctx, f.app, f.identifier, newUpload.Object.ID, strings.NewReader("later")))
	_, err = service.Complete(ctx, f.app, f.identifier, newUpload.Object.ID)
	require.NoError(t, err)
	found, err := service.Find(ctx, f.app, f.identifier, input.Namespace, input.Key)
	require.NoError(t, err)
	require.Equal(t, newUpload.Object.ID, found.ID)
	var queued int
	require.NoError(t, f.pool.QueryRow(ctx, "SELECT count(*) FROM build_cache_cleanup WHERE id = $1", upload.Object.ID).Scan(&queued))
	require.Equal(t, 1, queued)

	// A corrupt replacement must leave the last verified manifest available.
	input.SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte("third")))
	bad, err := service.Reserve(ctx, f.app, f.identifier, input)
	require.NoError(t, err)
	require.NoError(t, service.UploadLocal(ctx, f.app, f.identifier, bad.Object.ID, strings.NewReader("wrong")))
	_, err = service.Complete(ctx, f.app, f.identifier, bad.Object.ID)
	require.ErrorIs(t, err, services.ErrBuildCacheIntegrity)
	found, err = service.Find(ctx, f.app, f.identifier, input.Namespace, input.Key)
	require.NoError(t, err)
	require.Equal(t, newUpload.Object.ID, found.ID)

	// App deletion retains a bucket cleanup record after all foreign keys cascade.
	_, err = f.pool.Exec(ctx, "DELETE FROM apps WHERE id = $1", f.app)
	require.NoError(t, err)
	require.NoError(t, f.pool.QueryRow(ctx, "SELECT count(*) FROM build_cache_cleanup WHERE app_id = $1", f.app).Scan(&queued))
	require.Equal(t, 3, queued)
	_, err = f.pool.Exec(ctx, "UPDATE build_cache_cleanup SET due_at = now() WHERE app_id = $1", f.app)
	require.NoError(t, err)
	cleanup := services.NewBuildCleanup(f.pool, storage)
	_, err = cleanup.SweepCache(ctx)
	require.NoError(t, err)
	file, err := storage.GetBuildCache(ctx, bucket.BuildCacheObject{AppID: f.app, IdentifierID: f.identifier, Namespace: input.Namespace, ID: newUpload.Object.ID})
	require.NoError(t, err)
	require.Nil(t, file)
}

func TestBuildCacheConcurrentReservationsRespectQuota(t *testing.T) {
	f := setupBuildStore(t)
	ctx := context.Background()
	repo := store.NewPostgresBuildCacheStore(&database.Engine{Queries: pgdb.New(f.pool), DB: f.pool})
	var wg sync.WaitGroup
	errors := make(chan error, 24)
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.Reserve(ctx, types.BuildCacheObject{ID: uuid.NewString(), AppID: f.app, AppIdentifierID: f.identifier, Namespace: types.BuildCacheGradle, CacheKey: strings.Repeat("a", 32), Size: types.MaxBuildCacheObjectBytes, SHA256: strings.Repeat("b", 64)})
			errors <- err
		}()
	}
	wg.Wait()
	close(errors)
	accepted := 0
	for err := range errors {
		if err == nil {
			accepted++
		} else {
			require.ErrorIs(t, err, store.ErrBuildCacheFull)
		}
	}
	require.Equal(t, int(types.MaxBuildCacheBytes/types.MaxBuildCacheObjectBytes), accepted)
}

func TestBuildCacheRejectsUnknownNamespace(t *testing.T) {
	f := setupBuildStore(t)
	ctx := context.Background()
	repo := store.NewPostgresBuildCacheStore(&database.Engine{Queries: pgdb.New(f.pool), DB: f.pool})
	_, err := repo.Reserve(ctx, types.BuildCacheObject{ID: uuid.NewString(), AppID: f.app, AppIdentifierID: f.identifier, Namespace: "unknown", CacheKey: "key", Size: 1, SHA256: strings.Repeat("a", 64)})
	var constraint *pgconn.PgError
	require.ErrorAs(t, err, &constraint)
	require.Equal(t, "23514", constraint.Code)
	_, err = f.pool.Exec(ctx, "INSERT INTO build_cache_cleanup (id, app_id, app_identifier_id, namespace) VALUES ($1, $2, $3, 'unknown')", uuid.NewString(), f.app, f.identifier)
	require.ErrorAs(t, err, &constraint)
	require.Equal(t, "23514", constraint.Code)
}

// These requests start after authentication. The router tests cover build:create;
// here the real SQL must reject an upload ID outside that authenticated scope.
func TestBuildCacheHTTPRejectsForeignAndExpiredUploads(t *testing.T) {
	owner := setupBuildStore(t)
	otherApp := setupBuildStore(t)
	otherIdentifier := insertIdentifier(t, owner.identifiers, owner.app, types.PlatformAndroid, "com.example.other")
	ctx := context.Background()
	repo := store.NewPostgresBuildCacheStore(&database.Engine{Queries: pgdb.New(owner.pool), DB: owner.pool})
	storage := &bucket.LocalBucket{BasePath: t.TempDir()}
	service := services.NewBuildCacheService(repo, storage)
	handler := handlers.NewBuildCacheHandler(service)
	const content = "private compiled bytes"

	for _, scope := range []struct {
		name, app, identifier string
		expired               bool
	}{
		{"other app", otherApp.app, otherApp.identifier, false},
		{"other identifier", owner.app, otherIdentifier, false},
		{"expired", owner.app, owner.identifier, true},
	} {
		for _, published := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/published=%t", scope.name, published), func(t *testing.T) {
				input := services.BuildCacheInput{Namespace: types.BuildCacheCcache, Key: uuid.NewString(), Size: int64(len(content)), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(content)))}
				upload, err := service.Reserve(ctx, owner.app, owner.identifier, input)
				require.NoError(t, err)
				id := upload.Object.ID
				require.NoError(t, service.UploadLocal(ctx, owner.app, owner.identifier, id, strings.NewReader(content)))
				if published {
					_, err = service.Complete(ctx, owner.app, owner.identifier, id)
					require.NoError(t, err)
				}
				serve := func(method string, handle http.HandlerFunc, app, identifier string, body *strings.Reader) *httptest.ResponseRecorder {
					req := httptest.NewRequest(method, "/", body)
					req = mux.SetURLVars(req, map[string]string{"APP_ID": app, "UPLOAD_ID": id, "NAMESPACE": string(input.Namespace), "CACHE_KEY": input.Key})
					req = req.WithContext(services.WithBuildIdentifier(req.Context(), identifier))
					response := httptest.NewRecorder()
					handle(response, req)
					return response
				}
				// The rightful owner can read published bytes, but not a pending upload.
				response := serve(http.MethodGet, handler.Download, owner.app, owner.identifier, strings.NewReader(""))
				if published {
					require.Equal(t, http.StatusOK, response.Code, response.Body.String())
					require.Equal(t, content, response.Body.String())
				} else {
					require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
					require.NotContains(t, response.Body.String(), content)
				}
				if scope.expired {
					_, err = owner.pool.Exec(ctx, "UPDATE build_cache_objects SET expires_at = now() - interval '1 second' WHERE id = $1", id)
					require.NoError(t, err)
				}
				var before string
				require.NoError(t, owner.pool.QueryRow(ctx, "SELECT to_jsonb(c)::text FROM build_cache_objects c WHERE id = $1", id).Scan(&before))
				ref := bucket.BuildCacheObject{AppID: owner.app, IdentifierID: owner.identifier, Namespace: input.Namespace, ID: id}
				for _, operation := range []struct {
					name, method string
					handle       http.HandlerFunc
				}{
					{"upload", http.MethodPut, handler.UploadLocal},
					{"complete", http.MethodPost, handler.Complete},
					{"download", http.MethodGet, handler.Download},
					{"find", http.MethodGet, handler.Find},
				} {
					t.Run(operation.name, func(t *testing.T) {
						body := strings.NewReader("replacement bytes")
						response := serve(operation.method, operation.handle, scope.app, scope.identifier, body)
						require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
						require.NotContains(t, response.Body.String(), content)
						require.NotContains(t, response.Body.String(), input.SHA256)
						require.Equal(t, len("replacement bytes"), body.Len())
						var after string
						require.NoError(t, owner.pool.QueryRow(ctx, "SELECT to_jsonb(c)::text FROM build_cache_objects c WHERE id = $1", id).Scan(&after))
						require.Equal(t, before, after, "refused requests must not mutate the owner's cache entry")
						stored, err := os.ReadFile(filepath.Join(storage.BasePath, ref.Key()))
						require.NoError(t, err)
						require.Equal(t, content, string(stored))
						var queued int
						require.NoError(t, owner.pool.QueryRow(ctx, "SELECT count(*) FROM build_cache_cleanup WHERE id = $1", id).Scan(&queued))
						require.Zero(t, queued, "refused requests must not schedule deletion")
					})
				}
			})
		}
	}
}
