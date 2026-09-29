package appclient

// Failed HTTP decoding can populate fields before reporting an error.
// Registration callers receive either a complete successful value or its typed zero.
func registrationHTTPResult[T any](value T, err error) (T, error) {
	if err != nil {
		var zero T
		return zero, err
	}
	return value, nil
}
