package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/", handleRoot)
	r.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/check", handleCheck)

	addr := fmt.Sprintf(":%s", port)
	log.Printf("Сервер запущен на %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Ошибка запуска: %v", err)
	}
}

func handleRoot(c *gin.Context) {
	log.Printf("[REQUEST] GET / от клиента %s", c.ClientIP())

	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	exampleURL := fmt.Sprintf("%s://%s/check?host=192.168.0.10&port=3000", scheme, c.Request.Host)

	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Connection Checker</title></head>
<body>
<h1>Connection Checker</h1>
<p>Проверка TCP-соединения к указанному хосту и порту.</p>
<h3>Пример запроса:</h3>
<pre><a href="%s">%s</a></pre>
<h3>Параметры:</h3>
<ul>
  <li><b>host</b> — IP-адрес или домен (обязательный)</li>
  <li><b>port</b> — порт 1-65535 (обязательный)</li>
</ul>
</body>
</html>`, exampleURL, exampleURL)

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

func handleCheck(c *gin.Context) {
	log.Printf("[REQUEST] GET /check от клиента %s, параметры: %s", c.ClientIP(), c.Request.URL.RawQuery)

	host := c.Query("host")
	portStr := c.Query("port")

	if host == "" || portStr == "" {
		log.Printf("[REQUEST] Ошибка: не указаны обязательные параметры host/port")
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Обязательные параметры: host, port. Пример: /check?host=192.168.0.10&port=3000",
		})
		return
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		log.Printf("[REQUEST] Ошибка: некорректный порт %q", portStr)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Некорректный port (1-65535)"})
		return
	}

	addr := net.JoinHostPort(host, portStr)
	timeout := 5 * time.Second

	log.Printf("[CHECK] Начинаю TCP-соединение к %s (таймаут %s)...", addr, timeout)

	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, timeout)
	elapsed := time.Since(start)

	if err != nil {
		log.Printf("[CHECK] Хост %s НЕ ответил — ошибка: %s (время: %s)", addr, err, elapsed)
		c.JSON(http.StatusOK, gin.H{
			"host":    host,
			"port":    port,
			"success": false,
			"latency": elapsed.String(),
			"error":   err.Error(),
		})
		log.Printf("[CHECK] Отправил ответ клиенту: соединение к %s не удалось", addr)
		return
	}
	conn.Close()

	log.Printf("[CHECK] Хост %s ответил — соединение успешно установлено (время: %s)", addr, elapsed)
	c.JSON(http.StatusOK, gin.H{
		"host":    host,
		"port":    port,
		"success": true,
		"latency": elapsed.String(),
	})
	log.Printf("[CHECK] Отправил ответ клиенту: соединение к %s успешно", addr)
}
