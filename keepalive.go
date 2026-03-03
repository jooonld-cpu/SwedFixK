package main

import (
	"log"
	"net/http"
	"os"
	"time"
)

// StartKeepAlive — пингует сам себя каждые 10 минут чтобы Render не усыплял
func StartKeepAlive() {
	go func() {
		// Ждём 30 секунд пока сервер поднимется
		time.Sleep(30 * time.Second)

		appURL := os.Getenv("RENDER_EXTERNAL_URL")
		if appURL == "" {
			appURL = "https://swedfixk.onrender.com"
		}

		for {
			resp, err := http.Get(appURL + "/health")
			if err != nil {
				log.Println("⚠️ Keepalive ошибка:", err)
			} else {
				resp.Body.Close()
				log.Println("✅ Keepalive пинг OK")
			}
			time.Sleep(10 * time.Minute)
		}
	}()
}
