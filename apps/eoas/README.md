# EOAS

EOAS is a powerful helper package designed to simplify the setup and update publication process for the [xprem](https://github.com/mercuretechnologies/xprem) project.

## Quick Start

To get started with EOAS, check out the official documentation:
[EOAS Official Documentation](https://mercure-technologies.gitbook.io/xprem/eoas/overview)

## Android build cache

Android builds restore private Gradle and ccache archives before compilation and save them after the build artifact uploads successfully. During compilation, both caches use local files.

Use `--no-remote-cache` to build with the normal local Gradle configuration: no archive transfers, EOAS Gradle home, cache init script, ccache setup, or profiling. Gradle uses your `GRADLE_USER_HOME` or its usual `~/.gradle`; your own Gradle and compiler-cache settings still apply. This flag does not clear or disable caches you configured yourself.

Archives are scoped to the application identifier, build profile, host OS and architecture. Each build restores the latest archive in that scope; Gradle and ccache decide which entries still match the current sources and tools. The lockfile does not select the archive.

C/C++ caching requires ccache 4.11 or later. EOAS sets the standard `ANDROID_CCACHE` and CMake compiler-launcher environment variables. Explicit launcher settings take precedence. The default `CCACHE_SLOPPINESS=time_macros` caches compiled object files, including files that use precompiled headers, while leaving the large precompiled-header artifacts out of the cache. It ignores changes to time macros. An explicit `CCACHE_SLOPPINESS` value, including an empty string, overrides this default. Gradle caching works without ccache.

Each project's build workspace is recreated at a stable path so absolute paths do not change between builds on the same machine. One local build of that project can use it at a time. Different source or SDK paths between machines can still cause C/C++ cache misses.

Machine-specific ccache inode indexes and configuration are not reused from an archive. Before saving, ccache removes entries unused by that build and enforces a 450 MB limit. A ccache with no activity or unchanged entries keeps its previous archive. Published archives expire after 30 days. Download or upload failures produce a warning and leave the build usable.

Gradle uses a dedicated persistent home under `~/.eoas/.gradle` for each application and build profile. Only its native `build-cache-1` and `journal-1` directories are restored and archived. Dependencies are downloaded into this home on first use and retained locally.

Gradle 8.8 or later removes build-cache entries unused for three days through its public cleanup settings; older versions retain Gradle's default cleanup. Archives preserve Gradle’s journal and cleanup markers, and exclude locks, partial writes and failed entries. If the snapshot exceeds 450 MiB, EOAS skips publication and keeps the previous archive without deleting local entries. Native caching also works with Gradle's `--offline` option.

Project-specific cache configuration is respected; custom cache directories are not included in the archive. Settings and credentials in the usual `~/.gradle` are not automatically imported into the dedicated home.

Before publication, the server checks the archive's size, SHA-256 and TAR contents. Only regular files and directories with safe relative paths are accepted, with a maximum of 100,000 entries and 512 MiB per archive. Reservations are limited to 100 objects and 10 GiB per application identifier; rejected and replaced objects remain charged until bucket cleanup succeeds. These reservation limits do not bound the bytes a client can send directly to a signed bucket URL before validation.

## Learn More
For detailed information and to explore the core functionalities of xprem, visit the main repository:
[xprem on GitHub](https://github.com/mercuretechnologies/xprem)

---

Feel free to contribute, raise issues, or share feedback to help us improve EOAS!
