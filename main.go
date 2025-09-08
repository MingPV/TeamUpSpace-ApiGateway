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
		// you can adjust header or body response here
		return nil
	}

	// ========== User Service REST endpoints ==========
	mux.HandleFunc("/api/v1/auth/google/login", func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	})
	mux.HandleFunc("/auth/google/callback", func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	})
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

	log.Printf("Starting HTTP server on port %s", httpPort)
	return http.ListenAndServe(":"+httpPort, mux)
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
