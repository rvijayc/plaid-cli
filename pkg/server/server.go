package server

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"time"
)

type exchangeRequest struct {
	PublicToken string `json:"public_token"`
}

// StartServer starts a local HTTP server to host the Plaid Link flow.
// It returns the exchanged public token.
func StartServer(port int, linkToken string) (string, error) {
	tokenChan := make(chan string, 1)
	errChan := make(chan error, 1)

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		tmpl, err := template.New("index").Parse(htmlTemplate)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		data := struct {
			LinkToken string
		}{
			LinkToken: linkToken,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = tmpl.Execute(w, data)
	})

	mux.HandleFunc("/exchange", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req exchangeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		if req.PublicToken == "" {
			http.Error(w, "Missing public_token", http.StatusBadRequest)
			return
		}

		tokenChan <- req.PublicToken

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	server := &http.Server{
		Addr:    fmt.Sprintf("localhost:%d", port),
		Handler: mux,
	}

	go func() {
		listener, err := net.Listen("tcp", server.Addr)
		if err != nil {
			errChan <- fmt.Errorf("failed to listen on port %d: %w", port, err)
			return
		}
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("server error: %w", err)
		}
	}()

	select {
	case token := <-tokenChan:
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		return token, nil

	case err := <-errChan:
		return "", err

	case <-time.After(5 * time.Minute):
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		return "", fmt.Errorf("authentication timed out after 5 minutes")
	}
}

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Plaid CLI Authentication</title>
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@300;400;600;700&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg-color: #0b0f19;
            --card-bg: rgba(17, 24, 39, 0.7);
            --card-border: rgba(255, 255, 255, 0.08);
            --text-color: #f3f4f6;
            --text-muted: #9ca3af;
            --primary: #3b82f6;
            --primary-hover: #2563eb;
            --success: #10b981;
            --error: #ef4444;
            --glow: rgba(59, 130, 246, 0.15);
        }
        
        * {
            box-sizing: border-box;
            margin: 0;
            padding: 0;
        }
        
        body {
            font-family: 'Inter', -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
            background-color: var(--bg-color);
            color: var(--text-color);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            overflow: hidden;
            position: relative;
        }

        /* Animated background blob */
        body::before {
            content: '';
            position: absolute;
            width: 300px;
            height: 300px;
            background: radial-gradient(circle, var(--glow) 0%, transparent 70%);
            top: 20%;
            left: 20%;
            z-index: 0;
            animation: float 10s ease-in-out infinite alternate;
        }

        body::after {
            content: '';
            position: absolute;
            width: 400px;
            height: 400px;
            background: radial-gradient(circle, rgba(16, 185, 129, 0.1) 0%, transparent 70%);
            bottom: 20%;
            right: 20%;
            z-index: 0;
            animation: float 12s ease-in-out infinite alternate-reverse;
        }

        @keyframes float {
            0% { transform: translate(0, 0) scale(1); }
            100% { transform: translate(50px, 30px) scale(1.1); }
        }

        .container {
            position: relative;
            z-index: 1;
            width: 100%;
            max-width: 440px;
            padding: 20px;
        }

        .card {
            background: var(--card-bg);
            border: 1px solid var(--card-border);
            backdrop-filter: blur(12px);
            border-radius: 20px;
            padding: 40px 30px;
            box-shadow: 0 10px 25px -5px rgba(0, 0, 0, 0.3), 
                        0 8px 10px -6px rgba(0, 0, 0, 0.3),
                        0 0 40px var(--glow);
            text-align: center;
            transition: all 0.3s ease;
        }

        .logo-container {
            margin-bottom: 25px;
            display: flex;
            justify-content: center;
            align-items: center;
        }

        h1 {
            font-size: 24px;
            font-weight: 700;
            margin-bottom: 12px;
            letter-spacing: -0.5px;
            background: linear-gradient(to right, #ffffff, #9ca3af);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }

        p {
            font-size: 15px;
            color: var(--text-muted);
            line-height: 1.5;
            margin-bottom: 30px;
        }

        .status-box {
            display: flex;
            align-items: center;
            justify-content: center;
            gap: 10px;
            margin-bottom: 30px;
            font-size: 14px;
            color: var(--text-muted);
            padding: 10px 15px;
            background: rgba(255, 255, 255, 0.03);
            border-radius: 8px;
            border: 1px solid rgba(255, 255, 255, 0.05);
        }

        .spinner {
            width: 16px;
            height: 16px;
            border: 2px solid rgba(255, 255, 255, 0.1);
            border-top-color: var(--primary);
            border-radius: 50%;
            animation: spin 0.8s linear infinite;
        }

        @keyframes spin {
            to { transform: rotate(360deg); }
        }

        .btn {
            display: inline-flex;
            align-items: center;
            justify-content: center;
            width: 100%;
            padding: 14px 24px;
            background: var(--primary);
            color: white;
            font-size: 16px;
            font-weight: 600;
            border: none;
            border-radius: 12px;
            cursor: pointer;
            transition: all 0.2s ease;
            box-shadow: 0 4px 12px rgba(59, 130, 246, 0.3);
        }

        .btn:hover {
            background: var(--primary-hover);
            transform: translateY(-1px);
            box-shadow: 0 6px 16px rgba(59, 130, 246, 0.4);
        }

        .btn:active {
            transform: translateY(1px);
        }

        .btn:disabled {
            opacity: 0.6;
            cursor: not-allowed;
            transform: none;
            box-shadow: none;
        }

        .success-state h1 {
            background: linear-gradient(to right, #10b981, #059669);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }

        .error-state h1 {
            background: linear-gradient(to right, #ef4444, #dc2626);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="card" id="card">
            <div class="logo-container">
                <svg width="40" height="40" viewBox="0 0 100 100" fill="none" xmlns="http://www.w3.org/2000/svg">
                    <rect width="100" height="100" rx="20" fill="#111827"/>
                    <path d="M70 30H30V70H70V30Z" stroke="white" stroke-width="8" stroke-linejoin="round"/>
                    <path d="M50 30V70" stroke="white" stroke-width="8"/>
                    <path d="M30 50H70" stroke="white" stroke-width="8"/>
                </svg>
            </div>
            <div id="interactive-content">
                <h1>Plaid Authentication</h1>
                <p>Please complete authentication using Plaid Link to connect your bank accounts securely.</p>
                <div class="status-box" id="status-box">
                    <div class="spinner" id="spinner"></div>
                    <span id="status-text">Loading Plaid Link...</span>
                </div>
                <button class="btn" id="link-btn" onclick="openLink()" disabled>Connect Bank Account</button>
            </div>
        </div>
    </div>

    <script src="https://cdn.plaid.com/link/v2/stable/link-initialize.js"></script>
    <script>
        let handler = null;
        const linkToken = "{{.LinkToken}}";

        function updateStatus(text, showSpinner = true) {
            document.getElementById('status-text').innerText = text;
            document.getElementById('spinner').style.display = showSpinner ? 'block' : 'none';
        }

        function initPlaid() {
            if (!linkToken) {
                showError("Link token missing. Please restart the CLI.");
                return;
            }

            handler = Plaid.create({
                token: linkToken,
                onSuccess: function(public_token, metadata) {
                    sendToken(public_token);
                },
                onExit: function(err, metadata) {
                    if (err != null) {
                        showError("Plaid Link error: " + err.message);
                    } else {
                        updateStatus("Plaid Link closed. Click button to retry.", false);
                    }
                },
                onLoad: function() {
                    updateStatus("Ready to connect", false);
                    document.getElementById('link-btn').disabled = false;
                    openLink(); // Auto-open Link
                }
            });
        }

        function openLink() {
            if (handler) {
                handler.open();
                updateStatus("Authentication in progress...", true);
            }
        }

        function sendToken(publicToken) {
            updateStatus("Exchanging authentication token...", true);
            document.getElementById('link-btn').disabled = true;

            fetch('/exchange', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ public_token: publicToken })
            })
            .then(response => {
                if (response.ok) {
                    showSuccess();
                } else {
                    response.text().then(text => showError("Exchange failed: " + text));
                }
            })
            .catch(err => {
                showError("Network error: " + err.message);
            });
        }

        function showSuccess() {
            const card = document.getElementById('card');
            card.classList.add('success-state');
            card.innerHTML = '<div class="logo-container"><svg width="40" height="40" viewBox="0 0 100 100" fill="none" xmlns="http://www.w3.org/2000/svg"><rect width="100" height="100" rx="20" fill="#10B981"/><path d="M30 50L45 65L70 35" stroke="white" stroke-width="8" stroke-linecap="round" stroke-linejoin="round"/></svg></div><h1>Authentication Success!</h1><p>Your bank account has been connected successfully. You can now close this browser tab and return to the CLI.</p>';
        }

        function showError(message) {
            const card = document.getElementById('card');
            card.classList.add('error-state');
            card.innerHTML = '<div class="logo-container"><svg width="40" height="40" viewBox="0 0 100 100" fill="none" xmlns="http://www.w3.org/2000/svg"><rect width="100" height="100" rx="20" fill="#EF4444"/><path d="M35 35L65 65M65 35L35 65" stroke="white" stroke-width="8" stroke-linecap="round"/></svg></div><h1>Authentication Failed</h1><p>' + message + '</p><button class="btn" style="background: var(--error);" onclick="window.location.reload()">Try Again</button>';
        }

        window.onload = initPlaid;
    </script>
</body>
</html>
`
