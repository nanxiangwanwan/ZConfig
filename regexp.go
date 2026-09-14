package zconfig

// Common regular expressions for ConfigAttribute.RegExp.
const (
	// RegExpEmail accepts a conventional email address such as name@example.com.
	RegExpEmail = `^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$`

	// RegExpNonNegativeInt accepts 0 and positive integers, without leading zeroes.
	RegExpNonNegativeInt = `^(0|[1-9][0-9]*)$`

	// RegExpPositiveInt accepts positive integers only, without leading zeroes.
	RegExpPositiveInt = `^[1-9][0-9]*$`

	// RegExpFloat accepts signed decimal strings such as 1, -1.5, and .5.
	// Scientific notation is intentionally not included.
	RegExpFloat = `^[+-]?(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)$`

	// RegExpHTTPSURL accepts a conventional HTTPS URL, with an optional port,
	// path, query string, or fragment.
	RegExpHTTPSURL = `^https://[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?(?::[0-9]{1,5})?(?:[/?#][^\s]*)?$`
)
