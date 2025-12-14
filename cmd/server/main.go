package main

import (
	"context"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	chatpb "grpc-demo/gen/chat/v1"
	userpb "grpc-demo/gen/user/v1"
)

// ============================================================
// UserHandler
// ============================================================

type UserHandler struct {
	userpb.UnimplementedUserServiceServer
	mu    sync.RWMutex
	users map[string]*userpb.User
}

func NewUserHandler() *UserHandler {
	h := &UserHandler{
		users: make(map[string]*userpb.User),
	}

	// Тестовые пользователи
	h.users["1"] = &userpb.User{
		Id:        "1",
		Name:      "Иван Петров",
		Email:     "ivan@example.com",
		Age:       28,
		CreatedAt: timestamppb.Now(),
	}
	h.users["2"] = &userpb.User{
		Id:        "2",
		Name:      "Мария Сидорова",
		Email:     "maria@example.com",
		Age:       32,
		CreatedAt: timestamppb.Now(),
	}
	h.users["3"] = &userpb.User{
		Id:        "3",
		Name:      "Алексей Козлов",
		Email:     "alexey@example.com",
		Age:       25,
		CreatedAt: timestamppb.Now(),
	}

	return h
}

func (h *UserHandler) GetUser(ctx context.Context, req *userpb.GetUserRequest) (*userpb.GetUserResponse, error) {
	log.Printf("GetUser: id=%s", req.GetId())

	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	h.mu.RLock()
	user, ok := h.users[req.GetId()]
	h.mu.RUnlock()

	if !ok {
		return nil, status.Error(codes.NotFound, "user not found")
	}

	return &userpb.GetUserResponse{User: user}, nil
}

func (h *UserHandler) ListUsers(ctx context.Context, req *userpb.ListUsersRequest) (*userpb.ListUsersResponse, error) {
	log.Printf("ListUsers: page=%d, page_size=%d", req.GetPage(), req.GetPageSize())

	h.mu.RLock()
	defer h.mu.RUnlock()

	users := make([]*userpb.User, 0, len(h.users))
	for _, u := range h.users {
		users = append(users, u)
	}

	pageSize := int(req.GetPageSize())
	if pageSize <= 0 {
		pageSize = 10
	}

	page := int(req.GetPage())
	if page < 0 {
		page = 0
	}

	start := page * pageSize
	end := start + pageSize

	if start >= len(users) {
		return &userpb.ListUsersResponse{
			Users: []*userpb.User{},
			Total: int32(len(h.users)),
		}, nil
	}

	if end > len(users) {
		end = len(users)
	}

	return &userpb.ListUsersResponse{
		Users: users[start:end],
		Total: int32(len(h.users)),
	}, nil
}

func (h *UserHandler) CreateUser(ctx context.Context, req *userpb.CreateUserRequest) (*userpb.CreateUserResponse, error) {
	log.Printf("CreateUser: name=%s, email=%s, age=%d", req.GetName(), req.GetEmail(), req.GetAge())

	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if req.GetEmail() == "" {
		return nil, status.Error(codes.InvalidArgument, "email is required")
	}

	user := &userpb.User{
		Id:        uuid.New().String(),
		Name:      req.GetName(),
		Email:     req.GetEmail(),
		Age:       req.GetAge(),
		CreatedAt: timestamppb.Now(),
	}

	h.mu.Lock()
	h.users[user.Id] = user
	h.mu.Unlock()

	return &userpb.CreateUserResponse{User: user}, nil
}

// ============================================================
// ChatHandler
// ============================================================

type ChatHandler struct {
	chatpb.UnimplementedChatServiceServer
	hub *MessageHub
}

func NewChatHandler() *ChatHandler {
	return &ChatHandler{
		hub: NewMessageHub(),
	}
}

// SubscribeToRoom — Server Streaming
// Клиент подписывается, сервер шлёт сообщения
func (h *ChatHandler) SubscribeToRoom(req *chatpb.SubscribeRequest, stream chatpb.ChatService_SubscribeToRoomServer) error {
	roomID := req.GetRoomId()
	log.Printf("SubscribeToRoom: room_id=%s", roomID)

	ctx := stream.Context()
	messages := h.hub.Subscribe(roomID)
	defer h.hub.Unsubscribe(roomID, messages)

	// Отправляем приветственное сообщение
	welcome := &chatpb.ChatMessage{
		Id:        uuid.New().String(),
		RoomId:    roomID,
		UserId:    "system",
		Text:      "Добро пожаловать в комнату " + roomID + "! Ожидаем сообщения...",
		CreatedAt: timestamppb.Now(),
	}
	if err := stream.Send(welcome); err != nil {
		return err
	}

	// Запускаем генератор тестовых сообщений
	go h.generateTestMessages(ctx, roomID)

	for {
		select {
		case <-ctx.Done():
			log.Printf("SubscribeToRoom: клиент отключился от room_id=%s", roomID)
			return nil
		case msg := <-messages:
			log.Printf("SubscribeToRoom: отправляем сообщение в room_id=%s", roomID)
			if err := stream.Send(msg); err != nil {
				return err
			}
		}
	}
}

// generateTestMessages — генерирует тестовые сообщения каждые 3 секунды
func (h *ChatHandler) generateTestMessages(ctx context.Context, roomID string) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	testMessages := []string{
		"Привет всем!",
		"Как дела?",
		"Отличная погода сегодня",
		"Кто-нибудь онлайн?",
		"gRPC Streaming работает!",
	}

	i := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			msg := &chatpb.ChatMessage{
				Id:        uuid.New().String(),
				RoomId:    roomID,
				UserId:    "bot",
				Text:      testMessages[i%len(testMessages)],
				CreatedAt: timestamppb.Now(),
			}
			h.hub.Broadcast(roomID, msg)
			i++
		}
	}
}

// SendMessages — Client Streaming
// Клиент отправляет несколько сообщений, сервер отвечает один раз
func (h *ChatHandler) SendMessages(stream chatpb.ChatService_SendMessagesServer) error {
	log.Println("SendMessages: начинаем приём сообщений")

	var count int32

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			// Клиент закончил отправку
			log.Printf("SendMessages: получено %d сообщений", count)
			return stream.SendAndClose(&chatpb.SendMessagesResponse{
				ReceivedCount: count,
			})
		}
		if err != nil {
			return err
		}

		log.Printf("SendMessages: получено сообщение от %s: %s", msg.GetUserId(), msg.GetText())
		count++
	}
}

// Chat — Bidirectional Streaming
// Полноценный чат: сообщения в обе стороны
func (h *ChatHandler) Chat(stream chatpb.ChatService_ChatServer) error {
	log.Println("Chat: новое соединение")

	ctx := stream.Context()
	roomID := "global"

	messages := h.hub.Subscribe(roomID)
	defer h.hub.Unsubscribe(roomID, messages)

	// Горутина для отправки сообщений клиенту
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-messages:
				if err := stream.Send(msg); err != nil {
					return
				}
			}
		}
	}()

	// Отправляем приветствие
	welcome := &chatpb.ChatMessage{
		Id:        uuid.New().String(),
		RoomId:    roomID,
		UserId:    "system",
		Text:      "Добро пожаловать в чат! Отправьте сообщение.",
		CreatedAt: timestamppb.Now(),
	}
	if err := stream.Send(welcome); err != nil {
		return err
	}

	// Приём сообщений от клиента
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			log.Println("Chat: клиент закрыл соединение")
			return nil
		}
		if err != nil {
			return err
		}

		// Добавляем метаданные
		msg.Id = uuid.New().String()
		msg.CreatedAt = timestamppb.Now()
		if msg.RoomId == "" {
			msg.RoomId = roomID
		}

		log.Printf("Chat: получено от %s: %s", msg.GetUserId(), msg.GetText())

		// Рассылаем всем подписчикам (включая отправителя)
		h.hub.Broadcast(roomID, msg)
	}
}

// ============================================================
// MessageHub — хаб для рассылки сообщений
// ============================================================

type MessageHub struct {
	mu          sync.RWMutex
	subscribers map[string][]chan *chatpb.ChatMessage
}

func NewMessageHub() *MessageHub {
	return &MessageHub{
		subscribers: make(map[string][]chan *chatpb.ChatMessage),
	}
}

func (h *MessageHub) Subscribe(roomID string) chan *chatpb.ChatMessage {
	h.mu.Lock()
	defer h.mu.Unlock()

	ch := make(chan *chatpb.ChatMessage, 100)
	h.subscribers[roomID] = append(h.subscribers[roomID], ch)
	log.Printf("MessageHub: новый подписчик в комнате %s (всего: %d)", roomID, len(h.subscribers[roomID]))
	return ch
}

func (h *MessageHub) Unsubscribe(roomID string, ch chan *chatpb.ChatMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()

	subs := h.subscribers[roomID]
	for i, sub := range subs {
		if sub == ch {
			h.subscribers[roomID] = append(subs[:i], subs[i+1:]...)
			close(ch)
			log.Printf("MessageHub: подписчик отключился от комнаты %s", roomID)
			break
		}
	}
}

func (h *MessageHub) Broadcast(roomID string, msg *chatpb.ChatMessage) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, ch := range h.subscribers[roomID] {
		select {
		case ch <- msg:
		default:
			// Канал переполнен, пропускаем
		}
	}
}

// ============================================================
// Main
// ============================================================

func main() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	server := grpc.NewServer()

	// Регистрируем handlers
	userpb.RegisterUserServiceServer(server, NewUserHandler())
	chatpb.RegisterChatServiceServer(server, NewChatHandler())

	// Включаем Reflection для grpcurl и Postman
	reflection.Register(server)

	log.Println("gRPC server listening on :50051")
	log.Println("Reflection enabled")
	log.Println("")
	log.Println("Сервисы:")
	log.Println("  - user.v1.UserService (Unary)")
	log.Println("  - chat.v1.ChatService (Streaming)")

	if err := server.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
