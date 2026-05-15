package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type ProbeResult struct {
	Target   string `json:"target"`
	Port     int    `json:"port"`
	Success  bool   `json:"success"`
	Latency  string `json:"latency"`
	Error    string `json:"error,omitempty"`
	Verdict  string `json:"verdict"`
}

var defaultTargets = []struct {
	Host string
	Port int
}{
	{"192.168.0.10", 3000},
	{"192.168.0.10", 80},
	{"192.168.0.10", 443},
	{"192.168.0.10", 8080},
	{"192.168.0.10", 22},
	{"192.168.0.1", 3000},
	{"192.168.0.254", 3000},
	{"10.0.0.1", 3000},
	{"8.8.8.8", 53},
	{"8.8.8.8", 443},
}

func probe(host string, port int, timeout time.Duration) ProbeResult {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, timeout)
	elapsed := time.Since(start)

	res := ProbeResult{
		Target:  host,
		Port:    port,
		Latency: elapsed.String(),
	}

	if err != nil {
		res.Success = false
		res.Error = err.Error()
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			res.Verdict = "BLOCKED (timeout — похоже на фаервол DROP)"
		} else {
			res.Verdict = "REFUSED/UNREACHABLE (хост отклонил соединение или недоступен — обычно фаервол REJECT либо нет сервиса)"
		}
		return res
	}
	_ = conn.Close()
	res.Success = true
	res.Verdict = "OK (исходящий трафик разрешён, соединение установлено)"
	return res
}

func handleProbe(c *gin.Context) {
	host := c.Query("host")
	portStr := c.Query("port")
	timeoutStr := c.DefaultQuery("timeout", "3s")

	if host == "" || portStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "обязательные параметры: host, port. Пример: /probe?host=192.168.0.10&port=3000",
		})
		return
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректный port"})
		return
	}

	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		timeout = 3 * time.Second
	}

	log.Printf("[PROBE] -> %s:%d (timeout=%s)", host, port, timeout)
	res := probe(host, port, timeout)

	if res.Success {
		log.Printf("[PROBE] OK  %s:%d latency=%s — %s", host, port, res.Latency, res.Verdict)
	} else {
		log.Printf("[PROBE] FAIL %s:%d latency=%s err=%q — %s", host, port, res.Latency, res.Error, res.Verdict)
	}

	c.JSON(http.StatusOK, res)
}

func handleProbeAll(c *gin.Context) {
	timeoutStr := c.DefaultQuery("timeout", "3s")
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		timeout = 3 * time.Second
	}

	log.Printf("[PROBE-ALL] старт серии проверок (targets=%d, timeout=%s)", len(defaultTargets), timeout)

	results := make([]ProbeResult, len(defaultTargets))
	var wg sync.WaitGroup
	for i, t := range defaultTargets {
		wg.Add(1)
		go func(i int, host string, port int) {
			defer wg.Done()
			r := probe(host, port, timeout)
			results[i] = r
			if r.Success {
				log.Printf("[PROBE-ALL] OK   %s:%d latency=%s — %s", host, port, r.Latency, r.Verdict)
			} else {
				log.Printf("[PROBE-ALL] FAIL %s:%d latency=%s err=%q — %s", host, port, r.Latency, r.Error, r.Verdict)
			}
		}(i, t.Host, t.Port)
	}
	wg.Wait()

	allowed := 0
	blocked := 0
	for _, r := range results {
		if r.Success {
			allowed++
		} else {
			blocked++
		}
	}
	log.Printf("[PROBE-ALL] итог: разрешено=%d, заблокировано=%d", allowed, blocked)

	c.JSON(http.StatusOK, gin.H{
		"summary": gin.H{
			"total":   len(results),
			"allowed": allowed,
			"blocked": blocked,
		},
		"results": results,
	})
}

func handleRoot(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"service": "firewall-egress-tester",
		"usage": gin.H{
			"GET /":                                "эта подсказка",
			"GET /healthz":                         "liveness check",
			"GET /probe?host=H&port=P&timeout=3s":  "одиночная TCP-проверка исходящего соединения",
			"GET /probe-all?timeout=3s":            "набор проверок по списку дефолтных целей (см. логи)",
		},
		"default_targets": defaultTargets,
		"hint":            "ожидаемо: при ALLOW only 192.168.0.0/24:3000 должны пройти только пробы на эту подсеть и порт",
	})
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Printf("[HTTP] %s %s -> %d (%s) client=%s",
			c.Request.Method, c.Request.URL.RequestURI(),
			c.Writer.Status(), time.Since(start), c.ClientIP())
	})

	r.GET("/", handleRoot)
	r.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/probe", handleProbe)
	r.GET("/probe-all", handleProbeAll)

	addr := fmt.Sprintf(":%s", port)
	log.Printf("firewall-egress-tester слушает на %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("ошибка запуска сервера: %v", err)
	}
}
