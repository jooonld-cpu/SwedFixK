package main

import (
	"fmt"
	"net/http"
	"os"
)

// StartKeepAlive запускает легкий HTTP-сервер, чтобы Render не усыплял бота
func StartKeepAlive() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "10000" 
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Устанавливаем статус 200 OK
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Бот активен и слушает команды!")
	})

	go func() {
		fmt.Printf("Сервер анти-сна запущен на порту %s\n", port)
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			fmt.Printf("Ошибка сервера анти-сна: %v\n", err)
		}
	}()
}
