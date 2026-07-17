package inference

import "testing"

func TestNormalizeBaseURLDefaultsToLocalLMStudio(testContext *testing.T) {
	if received := NormalizeBaseURL(""); received != DefaultBaseURL {
		testContext.Fatalf("expected %q, received %q", DefaultBaseURL, received)
	}
}

func TestConfiguredBaseURLUsesEnvironmentValue(testContext *testing.T) {
	expected := "http://192.168.1.20:1234/v1"
	if received := ConfiguredBaseURL(expected + "/"); received != expected {
		testContext.Fatalf("expected %q, received %q", expected, received)
	}
}

func TestIsLoopbackBaseURL(testContext *testing.T) {
	localURLs := []string{
		"",
		"http://127.0.0.1:1234/v1",
		"http://localhost:11434/v1",
		"http://[::1]:8000/v1",
	}
	for _, baseURL := range localURLs {
		if !IsLoopbackBaseURL(baseURL) {
			testContext.Errorf("expected %q to be loopback", baseURL)
		}
	}

	remoteURLs := []string{
		"https://api.openai.com/v1",
		"https://localhost.example.com/v1",
		"not-a-url",
	}
	for _, baseURL := range remoteURLs {
		if IsLoopbackBaseURL(baseURL) {
			testContext.Errorf("expected %q to be remote or invalid", baseURL)
		}
	}
}
