package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrayServer_BearerTokenAuth(t *testing.T) {
	server, err := StartTrayServer(0)
	require.NoError(t, err)
	defer server.Close()

	baseURL := server.GetURL()
	token := server.GetAPIToken()
	require.NotEmpty(t, token)

	// 1. GET / must return index.html containing the injected session token in meta tag
	resp, err := http.Get(baseURL + "/")
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	expectedMeta := fmt.Sprintf(`<meta name="openuem-token" content="%s">`, token)
	assert.True(t, strings.Contains(string(body), expectedMeta), "HTML must contain injected auth token meta tag")

	// 2. GET /api/catalog without Authorization header must return 401 Unauthorized
	req, err := http.NewRequest("GET", baseURL+"/api/catalog", nil)
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "request without auth token must be rejected with 401")

	// 3. GET /api/catalog with invalid token must return 401 Unauthorized
	req, err = http.NewRequest("GET", baseURL+"/api/catalog", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer invalid-token-12345")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "request with wrong token must be rejected with 401")

	// 4. POST /api/install without token must return 401 Unauthorized
	req, err = http.NewRequest("POST", baseURL+"/api/install", strings.NewReader(`{"package_id":"some-pkg"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "install without token must be rejected with 401")
}
