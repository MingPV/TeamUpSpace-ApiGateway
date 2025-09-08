package main

import (
	"context"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	postpb "github.com/MingPV/ApiGateway/proto/post"
	profilepb "github.com/MingPV/ApiGateway/proto/profile"
)

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		// Remove any existing CORS headers to prevent duplication
		w.Header().Del("Access-Control-Allow-Origin")

		// allow origin
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		// handle preflight request
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func run() error {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	userServiceEndpoint := getEnv("USER_SERVICE_ENDPOINT", "localhost:50061")
	postServiceEndpoint := getEnv("POST_SERVICE_ENDPOINT", "localhost:50062")
	userServiceREST := getEnv("USER_SERVICE_REST", "http://localhost:8001") // REST port
	httpPort := getEnv("HTTP_PORT", "8080")

	// grpc-gateway ServeMux
	gwMux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}

	// ===== Register User Service =====
	if err := profilepb.RegisterProfileServiceHandlerFromEndpoint(ctx, gwMux, userServiceEndpoint, opts); err != nil {
		return err
	}

	// ===== Register Post Service =====
	if err := postpb.RegisterPostServiceHandlerFromEndpoint(ctx, gwMux, postServiceEndpoint, opts); err != nil {
		return err
	}

	// http.ServeMux for normal routes
	mux := http.NewServeMux()

	// create ReverseProxy for User Service REST endpoint
	userServiceURL, _ := url.Parse(userServiceREST)
	proxy := httputil.NewSingleHostReverseProxy(userServiceURL)
	// If you want to adjust path before forward
	proxy.ModifyResponse = func(resp *http.Response) error {
		// delete header CORS from backend to avoid duplication
		resp.Header.Del("Access-Control-Allow-Origin")
		resp.Header.Del("Access-Control-Allow-Methods")
		resp.Header.Del("Access-Control-Allow-Headers")
		resp.Header.Del("Access-Control-Allow-Credentials")
		return nil
	}

	// ========== User Service REST endpoints ==========
	mux.HandleFunc("/api/v1/auth/google/login", func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	})
	mux.HandleFunc("/auth/google/callback", func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	})
	mux.HandleFunc("/api/v1/auth/signin", func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	})
	mux.HandleFunc("/api/v1/auth/signup", func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	})
	mux.HandleFunc("/api/v1/auth/signout", func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	})
	// Don't forget to put / at the end to get all subpath such as /users/{id} /username/{username}
	mux.HandleFunc("/api/v1/users/", func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	})
	mux.HandleFunc("/api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	})
	// ================================================

	// ========== gRPC ==========
	// other Routes go to grpc-gateway
	mux.Handle("/", gwMux)
	// ==========================

	handlerWithCORS := corsMiddleware(mux)
	log.Printf("Starting HTTP server on port %s", httpPort)
	log.Printf("User Service gRPC endpoint: %s", userServiceEndpoint)
	log.Printf("Post Service gRPC endpoint: %s", postServiceEndpoint)
	log.Printf("User Service REST endpoint: %s", userServiceREST)

	return http.ListenAndServe(":"+httpPort, handlerWithCORS)
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
