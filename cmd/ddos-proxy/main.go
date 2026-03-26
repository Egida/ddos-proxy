package main

import (
	"context"
	"html/template"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hegy/ddos-proxy/internal/config"
	"github.com/hegy/ddos-proxy/internal/limiter"
	"github.com/hegy/ddos-proxy/internal/metrics"
	"github.com/hegy/ddos-proxy/internal/proxy"
	"github.com/hegy/ddos-proxy/internal/waf"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load configuration", "error", "PROXY_BACKEND_URL is required")
		os.Exit(1)
	}

	// Parse the backend URL.
	targetURL, err := url.Parse(cfg.BackendURL)
	if err != nil {
		slog.Error("Invalid backend URL", "url", cfg.BackendURL, "error", err)
		os.Exit(1)
	}

	// Load templates
	tmpl, err := template.ParseFiles("challenge.html")
	if err != nil {
		slog.Error("Failed to load templates", "error", err)
		os.Exit(1)
	}

	rl := limiter.New()
	wafManager := waf.NewManager(cfg, rl, tmpl)

	// Start rate limiter reset ticker
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			rl.Reset()
		}
	}()

	var reverseProxy *httputil.ReverseProxy

	if cfg.UseVarnish {
		directProxy := proxy.New(targetURL, targetURL.Host)

		internalProxy := httputil.NewSingleHostReverseProxy(targetURL)

		internalProxy.Transport = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			DisableKeepAlives:     true,
		}

		originalDirector := internalProxy.Director
		internalProxy.Director = func(req *http.Request) {
			originalHost := req.Host
			originalDirector(req)
			req.Host = originalHost
			req.Header.Del("Connection")
			req.Header.Del("Keep-Alive")
		}

		internalServer := &http.Server{
			Addr:              "127.0.0.1:6082",
			Handler:           internalProxy,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       120 * time.Second,
		}

		go func() {
			slog.Info("Starting internal proxy for Varnish", "addr", "127.0.0.1:6082", "backend", cfg.BackendURL)
			if err := internalServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("Internal server failed", "error", err)
			}
		}()

		varnishURL, _ := url.Parse("http://127.0.0.1:6081")
		reverseProxy = proxy.New(varnishURL, targetURL.Host)

		reverseProxy.Transport = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			DisableKeepAlives:     true,
		}
		reverseProxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "127.0.0.1:6081") ||
				strings.Contains(errStr, "connection refused") ||
				strings.Contains(errStr, "connection reset") ||
				strings.Contains(errStr, "broken pipe") ||
				strings.Contains(errStr, "timeout") {
				slog.Warn("Varnish unavailable, falling back to direct backend proxy", "error", err, "path", r.URL.Path)
				directProxy.ServeHTTP(w, r)
				return
			}
			slog.Error("Proxy error", "error", err, "path", r.URL.Path)
			http.Error(w, "Bad Gateway", http.StatusBadGateway)
		}

	} else {
		reverseProxy = proxy.New(targetURL, targetURL.Host)
	}

	handler := wafManager.Middleware(reverseProxy)

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if cfg.UseVarnish && r.Method == "PURGE" {
			if cfg.VarnishPurgeKey == "" || r.Header.Get("X-Purge-Key") != cfg.VarnishPurgeKey {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			purgeURL := "http://127.0.0.1:6081/" + r.URL.Path
			if r.URL.RawQuery != "" {
				purgeURL += "?" + r.URL.RawQuery
			}
			purgeReq, err := http.NewRequest("PURGE", purgeURL, nil)
			if err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			purgeReq.Host = r.Host

			purgeClient := &http.Client{Timeout: 5 * time.Second}
			resp, err := purgeClient.Do(purgeReq)
			if err != nil {
				http.Error(w, "Gateway Timeout", http.StatusGatewayTimeout)
				return
			}
			defer resp.Body.Close()

			for k, vv := range resp.Header {
				for _, v := range vv {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(resp.StatusCode)
			io.Copy(w, resp.Body)
			return
		}
		handler.ServeHTTP(w, r)
	})

	if cfg.PrometheusEnabled {
		metricsLimiter := limiter.NewIPLimiter()
		metricsHandler := promhttp.Handler()
		mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			if !metricsLimiter.Allow(ip) {
				metrics.DroppedRequests.WithLabelValues("metrics_rate_limit").Inc()
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}
			metricsHandler.ServeHTTP(w, r)
		})
		slog.Info("Prometheus metrics enabled", "endpoint", "/metrics")
	}

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		ConnState: func(conn net.Conn, state http.ConnState) {
			if state == http.StateNew {
				rl.IncConn()
			}
		},
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("Starting proxy server",
			"port", cfg.Port,
			"backend", cfg.BackendURL,
			"max_req_per_sec", cfg.MaxReqPerSec,
			"max_conn_per_sec", cfg.MaxConnPerSec,
			"mitigation_time", cfg.MitigationTime,
			"always_on", cfg.AlwaysOn,
			"prometheus_enabled", cfg.PrometheusEnabled,
		)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-stop
	slog.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
	}

	slog.Info("Server exited properly")
}
