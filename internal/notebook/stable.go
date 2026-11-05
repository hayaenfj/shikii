package notebook

// WithStablePages serializes verification and publication with article changes.
// The action must be a short storage operation without model calls or wiki writes.
// Callers that also lock a session acquire the session before this page lock.
func (b Book) WithStablePages(action func() error) error {
	dir, err := b.directory(true, "pages")
	if err != nil {
		return err
	}
	unlock, err := lock(dir)
	if err != nil {
		return err
	}
	defer unlock()
	if action != nil {
		return action()
	}
	return nil
}
