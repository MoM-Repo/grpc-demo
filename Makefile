.PHONY: generate run test

# Генерация Go кода из proto
generate:
	buf generate

# Запуск сервера
run:
	go run cmd/server/main.go

# Тест через grpcurl
test-list:
	grpcurl -plaintext localhost:50051 list

test-get:
	grpcurl -plaintext -d '{"id": "1"}' localhost:50051 user.v1.UserService/GetUser

test-list-users:
	grpcurl -plaintext -d '{"page": 0, "page_size": 10}' localhost:50051 user.v1.UserService/ListUsers

test-create:
	grpcurl -plaintext -d '{"name": "Новый пользователь", "email": "new@example.com", "age": 30}' localhost:50051 user.v1.UserService/CreateUser

