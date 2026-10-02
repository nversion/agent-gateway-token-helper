package browser

func command(url string) (string, []string) {
	// rundll32 hands the URL to the default handler without going through
	// cmd.exe, whose quoting would mangle the & in query strings.
	return "rundll32", []string{"url.dll,FileProtocolHandler", url}
}
