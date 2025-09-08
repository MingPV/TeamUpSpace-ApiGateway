package main

import (
	"context"
	"log"
	"net/http"
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
	// eventServiceEndpoint := getEnv("POST_SERVICE_ENDPOINT", "localhost:50062")
	// chatServiceEndpoint := getEnv("POST_SERVICE_ENDPOINT", "localhost:50062")
	// notificationServiceEndpoint := getEnv("POST_SERVICE_ENDPOINT", "localhost:50062")
	httpPort := getEnv("HTTP_PORT", "8080")

	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}

	// Register profile UserService
	if err := profilepb.RegisterProfileServiceHandlerFromEndpoint(ctx, mux, userServiceEndpoint, opts); err != nil {
		return err
	}

	// Register post PostService
	if err := postpb.RegisterPostServiceHandlerFromEndpoint(ctx, mux, postServiceEndpoint, opts); err != nil {
		return err
	}

	log.Printf("Starting HTTP server on port %s", httpPort)
	log.Printf("Proxying requests to userService server at %s", userServiceEndpoint)
	log.Printf("Proxying requests to postService server at %s", postServiceEndpoint)

	return http.ListenAndServe(":"+httpPort, mux)
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
