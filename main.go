package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	eventpb "github.com/MingPV/ApiGateway/proto/event"
	eventtagpb "github.com/MingPV/ApiGateway/proto/eventTag"

	// notificationpb "github.com/MingPV/ApiGateway/proto/notification"
	postpb "github.com/MingPV/ApiGateway/proto/post"
	profilepb "github.com/MingPV/ApiGateway/proto/profile"
	tagpb "github.com/MingPV/ApiGateway/proto/tag"
)

	messagepb "github.com/MingPV/ApiGateway/proto/message"
	"github.com/gorilla/websocket"
)

// handleWebSocketConnection manages a single WebSocket connection for a chat room
func handleWebSocketConnection(conn *websocket.Conn, roomID int, chatClient messagepb.MessageServiceClient) {
	defer func() {
		log.Printf("WS: room=%d disconnect", roomID)
		conn.Close()
	}()

	ictx, icancel := context.WithCancel(context.Background())
	defer icancel()

	stream, err := chatClient.Chat(ictx)
	if err != nil { 
		log.Printf("WS: chat connect error: %v", err)
		conn.WriteMessage(websocket.TextMessage, []byte("stream connect error"))
		return 
	}
	defer stream.CloseSend()

	if err := stream.Send(&messagepb.ClientEvent{Payload: &messagepb.ClientEvent_Join{Join: &messagepb.JoinRoom{RoomId: uint32(roomID)}}}); err != nil {
		log.Printf("WS: join send error: %v", err)
		return
	}

	type inbound struct {
		Message string `json:"message"`
		Sender  string `json:"sender"`
		SentAt  int64  `json:"sent_at_unix"`
	}

	done := make(chan struct{})

	// Goroutine to handle incoming messages from WebSocket
	go func() {
		defer close(done)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				log.Printf("WS: room=%d read error: %v", roomID, err)
				return
			}
			var in inbound
			if err := json.Unmarshal(data, &in); err != nil { 
				log.Printf("WS: room=%d unmarshal error: %v", roomID, err)
				continue 
			}
			if in.SentAt == 0 { 
				in.SentAt = time.Now().Unix() 
			}
			if err := stream.Send(&messagepb.ClientEvent{Payload: &messagepb.ClientEvent_Send{Send: &messagepb.SendMessage{ 
				RoomId: uint32(roomID), 
				Text: in.Message, 
				SenderId: in.Sender, 
				SentAtUnix: in.SentAt,
			}}}); err != nil {
				log.Printf("WS: room=%d send error: %v", roomID, err)
				return
			}
		}
	}()

	// Goroutine to handle outgoing messages from gRPC stream
	go func() {
		for {
			ev, err := stream.Recv()
			if err != nil {
				log.Printf("WS: room=%d stream recv error: %v", roomID, err)
				conn.WriteMessage(websocket.TextMessage, []byte("stream closed"))
				return
			}
			b, _ := json.Marshal(ev)
			if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
				log.Printf("WS: room=%d write error: %v", roomID, err)
				return
			}
		}
	}()

	// Wait for either goroutine to finish, then clean up
	<-done
}

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
	eventServiceEndpoint := getEnv("EVENT_SERVICE_ENDPOINT", "localhost:50063")
	// notificationServiceEndpoint := getEnv("NOTIFICATION_SERVICE_ENDPOINT", "localhost:50065")
	userServiceREST := getEnv("USER_SERVICE_REST", "http://localhost:8001") // REST port
	chatServiceEndpoint := getEnv("CHAT_SERVICE_ENDPOINT", "localhost:50064")
	httpPort := getEnv("HTTP_PORT", "8080")

	// grpc-gateway ServeMux
	gwMux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}

	// ===== Register gRPC User Service =====
	if err := profilepb.RegisterProfileServiceHandlerFromEndpoint(ctx, gwMux, userServiceEndpoint, opts); err != nil {
		return err
	}

	// ===== Register gRPC Post Service =====
	if err := postpb.RegisterPostServiceHandlerFromEndpoint(ctx, gwMux, postServiceEndpoint, opts); err != nil {
		return err
	}

	// ===== Register gRPC Event Service =====
	if err := eventpb.RegisterEventServiceHandlerFromEndpoint(ctx, gwMux, eventServiceEndpoint, opts); err != nil {
		return err
	}
	if err := eventtagpb.RegisterEventTagServiceHandlerFromEndpoint(ctx, gwMux, eventServiceEndpoint, opts); err != nil {
		return err
	}
	if err := tagpb.RegisterTagServiceHandlerFromEndpoint(ctx, gwMux, eventServiceEndpoint, opts); err != nil {
		return err
	}

	// ===== Register gRPC Notification Service =====
	// if err := notificationpb.RegisterNotificationServiceHandlerFromEndpoint(ctx, gwMux, notificationServiceEndpoint, opts); err != nil {
	// 	return err
	// }

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

	// ========== WebSocket Chat Gateway ==========
	chatConn, err := grpc.Dial(chatServiceEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	chatClient := messagepb.NewMessageServiceClient(chatConn)

	mux.HandleFunc("/api/v1/ws/rooms/", func(w http.ResponseWriter, r *http.Request) {
		// Expect /api/v1/ws/rooms/{roomId}
		suffix := strings.TrimPrefix(r.URL.Path, "/api/v1/ws/rooms/")
		roomID, _ := strconv.Atoi(suffix)
		if roomID <= 0 {
			http.Error(w, "invalid room id", http.StatusBadRequest)
			return
		}
		upgrader := websocket.Upgrader{ CheckOrigin: func(r *http.Request) bool { return true } }
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil { 
			log.Printf("WS: upgrade error: %v", err)
			return 
		}

		log.Printf("WS: room=%d connect", roomID)

		// Start the WebSocket connection in a separate goroutine
		// This prevents the handler from blocking the HTTP server
		go handleWebSocketConnection(conn, roomID, chatClient)
	})

	// ========== gRPC ==========
	// other Routes go to grpc-gateway
	mux.Handle("/", gwMux)
	// ==========================

	handlerWithCORS := corsMiddleware(mux)
	
	// Create HTTP server with timeouts
	server := &http.Server{
		Addr:         ":" + httpPort,
		Handler:      handlerWithCORS,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("Starting HTTP server on port %s", httpPort)
	log.Printf("User Service gRPC endpoint: %s", userServiceEndpoint)
	log.Printf("Post Service gRPC endpoint: %s", postServiceEndpoint)
	log.Printf("User Service REST endpoint: %s", userServiceREST)
	log.Printf("Chat Service gRPC endpoint: %s", chatServiceEndpoint)

	// Start server in a goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Give outstanding requests 30 seconds to complete
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
