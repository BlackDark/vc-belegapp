package erkennung

// Active reports whether extraction will call a model.
// A nil extractor and the disabled implementation are inactive.
func Active(ex ReceiptExtractor) bool {
	if ex == nil {
		return false
	}
	switch ex.(type) {
	case Disabled, *Disabled:
		return false
	default:
		return true
	}
}
