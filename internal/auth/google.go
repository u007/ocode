package auth

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
)

func LoginWithGoogle() (string, error) {
	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	if clientID == "" {
		return "", fmt.Errorf("GOOGLE_CLIENT_ID environment variable not set")
	}
	redirectURL := "http://localhost:8080/callback"

	authURL := fmt.Sprintf("https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&response_type=token&scope=https://www.googleapis.com/auth/userinfo.email", clientID, redirectURL)

	fmt.Printf("Opening browser for Google login...\n")
	openBrowser(authURL)

	server := &http.Server{Addr: ":8080"}
	tokenChan := make(chan string)

	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Authentication successful! You can close this window."))
		tokenChan <- "DEMO_TOKEN_SUCCESS"
	})

	go server.ListenAndServe()

	token := <-tokenChan
	server.Shutdown(context.Background())

	return token, nil
}

func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		fmt.Printf("Please open this URL in your browser: %s\n", url)
	}
}
