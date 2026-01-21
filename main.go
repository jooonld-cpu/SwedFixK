package main

import (
    "context"
    "fmt"
    "github.com/jackc/pgx/v5"
    "os"
)

func ConnectDB() (*pgx.Conn, error) {
    // На Render лучше всего использовать переменные окружения,
    // но для начала можно вписать напрямую:
    host     := "db.bnjeilatyhxttfgnvwvd.supabase.co"
    port     := "5432"
    user     := "postgres"
    password := "9vX4kLp2mQ7nZtR1wB5y8Hj"
    dbname   := "postgres"

    // Формируем строку подключения. 
    // На Render sslmode=require обязателен для Supabase!
    connString := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=require", 
        user, password, host, port, dbname)

    conn, err := pgx.Connect(context.Background(), connString)
    if err != nil {
        return nil, fmt.Errorf("не удалось подключиться к БД: %v", err)
    }

    fmt.Println("✅ Успешно подключено к Supabase!")
    return conn, nil
}