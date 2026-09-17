package serverapp

// chainCleanup runs next before prev. Registering a shutdown step by assigning
// to cleanup drops whatever was registered before it; going through here makes
// that mistake impossible to write.
func chainCleanup(prev, next func()) func() {
	return func() {
		next()
		prev()
	}
}
