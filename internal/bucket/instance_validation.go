package bucket

func (v *validatingBucket) GetInstanceID() (string, error) {
	return v.Inner.GetInstanceID()
}

func (v *validatingBucket) PersistInstanceID(id string) error {
	return v.Inner.PersistInstanceID(id)
}
