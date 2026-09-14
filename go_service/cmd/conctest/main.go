package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

func getToken() string {
	body, _ := json.Marshal(map[string]string{"app_key": "demo_key", "app_secret": "demo_secret"})
	resp, err := http.Post("http://127.0.0.1:8080/api/auth/token", "application/json", bytes.NewReader(body))
	if err != nil {
		return ""
	}
	var tr struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&tr)
	resp.Body.Close()
	return tr.Data.Token
}

func runOne(token string, idx int, wg *sync.WaitGroup, results chan<- string) {
	defer wg.Done()
	user := fmt.Sprintf("t_demo:conc_%d_%d", idx, time.Now().UnixNano())
	conn, _, err := websocket.DefaultDialer.Dial(
		"ws://127.0.0.1:8080/ws/chat?token="+token+"&user_id="+user, nil)
	if err != nil {
		results <- fmt.Sprintf("[%d] dial err: %v", idx, err)
		return
	}
	defer conn.Close()
	_ = conn.WriteJSON(map[string]string{"type": "chat", "msg": fmt.Sprintf("conc test %d", idx)})
	deadline := time.Now().Add(60 * time.Second)
	gotEnd := false
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		var m map[string]interface{}
		if err := conn.ReadJSON(&m); err != nil {
			break
		}
		if m["type"] == "chat_end" {
			gotEnd = true
			break
		}
	}
	results <- fmt.Sprintf("[%d] chat_end=%v", idx, gotEnd)
}

func main() {
	token := getToken()
	if token == "" {
		fmt.Println("token fail")
		os.Exit(1)
	}
	fmt.Println("token ok, 并发 5 用户...")
	var wg sync.WaitGroup
	results := make(chan string, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go runOne(token, i, &wg, results)
	}
	wg.Wait()
	close(results)
	for r := range results {
		fmt.Println(r)
	}
}
