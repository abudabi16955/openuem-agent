package agent

import (
	"bytes"
	"strings"
)

// UTF8BOM represents the 3-byte UTF-8 Byte Order Mark ([]byte{0xEF, 0xBB, 0xBF}).
var UTF8BOM = []byte{0xEF, 0xBB, 0xBF}

// PowerShellUTF8OutputEncodingCommand is injected at the beginning of PowerShell scripts
// to ensure both console output and pipeline output streams are encoded in UTF-8.
const PowerShellUTF8OutputEncodingCommand = "$OutputEncoding = [Console]::OutputEncoding = [System.Text.Encoding]::UTF8"

// preparePowerShellScript formats a PowerShell script for safe and correct execution on Windows:
// 1. Prepends the UTF-8 BOM so Windows PowerShell parses Unicode/national characters (e.g. Polish diacritics) correctly.
// 2. Injects the UTF-8 output encoding statement to force stdout and stderr streams to UTF-8.
// 3. Normalizes line breaks to CRLF (\r\n) so multiline scripts and block constructs (if/else, switch) maintain proper syntax.
func preparePowerShellScript(script string) []byte {
	// Strip leading BOM if already present to avoid duplicate BOMs
	script = strings.TrimPrefix(script, "\xef\xbb\xbf")

	// Normalize line breaks to CRLF (\r\n)
	normalized := strings.ReplaceAll(script, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\n", "\r\n")

	var buf bytes.Buffer
	buf.Write(UTF8BOM)
	buf.WriteString(PowerShellUTF8OutputEncodingCommand)
	buf.WriteString("\r\n")
	buf.WriteString(normalized)
	if !strings.HasSuffix(normalized, "\r\n") {
		buf.WriteString("\r\n")
	}

	return buf.Bytes()
}
