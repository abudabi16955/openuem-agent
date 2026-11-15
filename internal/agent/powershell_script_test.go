package agent

import (
	"bytes"
	"strings"
	"testing"
)

func TestPreparePowerShellScript_UTF8BOM(t *testing.T) {
	script := "Write-Output 'Hello World'"
	res := preparePowerShellScript(script)

	if len(res) < 3 {
		t.Fatalf("prepared script is too short: %d bytes", len(res))
	}

	expectedBOM := []byte{0xEF, 0xBB, 0xBF}
	if !bytes.Equal(res[:3], expectedBOM) {
		t.Errorf("expected script to start with UTF-8 BOM (%X), got %X", expectedBOM, res[:3])
	}
}

func TestPreparePowerShellScript_OutputEncodingHeader(t *testing.T) {
	script := "Write-Output 'Testing Encoding'"
	res := preparePowerShellScript(script)

	// Content after the 3-byte BOM
	contentAfterBOM := string(res[3:])
	expectedHeader := "$OutputEncoding = [Console]::OutputEncoding = [System.Text.Encoding]::UTF8\r\n"

	if !strings.HasPrefix(contentAfterBOM, expectedHeader) {
		t.Errorf("expected script to start with encoding header %q, got prefix %q", expectedHeader, contentAfterBOM[:min(len(contentAfterBOM), len(expectedHeader))])
	}
}

func TestPreparePowerShellScript_PolishDiacritics(t *testing.T) {
	polishText := "Zażółć gęślą jaźń: ąćęłńóśźż ĄĆĘŁŃÓŚŹŻ"
	script := "Write-Output \"" + polishText + "\""
	res := preparePowerShellScript(script)

	// Check that the BOM is intact
	if !bytes.Equal(res[:3], UTF8BOM) {
		t.Errorf("missing UTF-8 BOM")
	}

	content := string(res[3:])
	if !strings.Contains(content, polishText) {
		t.Errorf("expected script content to contain Polish diacritics verbatim %q, got: %q", polishText, content)
	}
}

func TestPreparePowerShellScript_MultilineAndControlStructures(t *testing.T) {
	multilineScript := strings.Join([]string{
		`$condition = $true`,
		`if ($condition) {`,
		`    Write-Output "Condition met: ąćę"`,
		`} else {`,
		`    Write-Output "Condition failed"`,
		`}`,
		`switch ("test") {`,
		`    "test" { Write-Output "Match: łóś" }`,
		`    default { Write-Output "Default" }`,
		`}`,
	}, "\n") // deliberately using Unix LF to test normalization

	res := preparePowerShellScript(multilineScript)
	content := string(res[3:])

	// Must contain CRLF, and no orphan LF
	lines := strings.Split(content, "\r\n")
	for i, line := range lines {
		if strings.Contains(line, "\n") {
			t.Errorf("line %d contains unnormalized LF: %q", i, line)
		}
	}

	// Verify structure is preserved (not flattened into one line)
	expectedSubstrings := []string{
		"if ($condition) {\r\n",
		"} else {\r\n",
		"switch (\"test\") {\r\n",
		"Condition met: ąćę",
		"Match: łóś",
	}

	for _, sub := range expectedSubstrings {
		if !strings.Contains(content, sub) {
			t.Errorf("multiline content missing expected block structure: %q", sub)
		}
	}
}

func TestPreparePowerShellScript_ExistingBOMHandling(t *testing.T) {
	scriptWithBOM := "\xef\xbb\xbfWrite-Output 'already has BOM'"
	res := preparePowerShellScript(scriptWithBOM)

	// First 3 bytes must be BOM
	if !bytes.Equal(res[:3], UTF8BOM) {
		t.Fatalf("missing UTF-8 BOM at start")
	}

	// Subsequent bytes must NOT contain a second BOM
	contentAfterBOM := string(res[3:])
	if strings.Contains(contentAfterBOM, "\xef\xbb\xbf") {
		t.Errorf("script contains duplicate UTF-8 BOM in content")
	}
}

func TestPreparePowerShellScript_TrailingNewline(t *testing.T) {
	tests := []struct {
		name   string
		script string
	}{
		{name: "no newline", script: "Write-Output 'one'"},
		{name: "with LF", script: "Write-Output 'one'\n"},
		{name: "with CRLF", script: "Write-Output 'one'\r\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := preparePowerShellScript(tt.script)
			if !bytes.HasSuffix(res, []byte("\r\n")) {
				t.Errorf("expected script to end with CRLF (\\r\\n)")
			}
		})
	}
}
