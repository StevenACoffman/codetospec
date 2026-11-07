package policy

// ValidateAgainst checks data against the named embedded schema, for the
// external tests of what the package writes.
func ValidateAgainst(name string, data []byte) error {
	_, err := validated(name, data)
	return err
}
