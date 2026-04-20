package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"sync"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type Client struct {
	conn     *websocket.Conn
	send     chan []byte
	nickname string
}

type ChatRoom struct {
	clients    *sync.Map
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client

	maxClients int

	history [][]byte
}

func NewChatRoom() *ChatRoom {
	return &ChatRoom{
		clients:    &sync.Map{},
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		maxClients: 30,
		history:    make([][]byte, 0),
	}
}

// Run client registration and broadcasting
func (cr *ChatRoom) Run() {
	for {
		select {
		case client := <-cr.register:
			cr.clients.Store(client, true)
			for _, msg := range cr.history {
				client.send <- msg
			}
			cr.sendToAll([]byte(client.nickname + " joined the chat"))
		case client := <-cr.unregister:
			if _, ok := cr.clients.LoadAndDelete(client); ok {
				close(client.send)
			}
			cr.sendToAll([]byte(client.nickname + " left the chat"))
		case message := <-cr.broadcast:
			cr.history = append(cr.history, message)

			// limit history size
			if len(cr.history) > 50 {
				cr.history = cr.history[1:]
			}

			cr.sendToAll(message)
		}
	}
}

func (cr *ChatRoom) sendToAll(message []byte) {
	cr.clients.Range(func(k, v interface{}) bool {
		client := k.(*Client)
		select {
		case client.send <- message:
		default:
			cr.clients.Delete(client)
			close(client.send)
		}
		return true
	})
}

func (cr *ChatRoom) HandleWebSocket(g *gin.Context) {
	conn, err := upgrader.Upgrade(g.Writer, g.Request, nil)
	if err != nil {
		log.Println("upgrade error:", err)
		return
	}

	nickname := g.Query("nickname")
	if nickname == "" {
		nickname = "Anonymous"
	}

	client := &Client{conn: conn, send: make(chan []byte, 256), nickname: nickname}
	cr.register <- client

	go client.write()
	go client.read(cr)
}

func (c *Client) write() {
	defer c.conn.Close()
	for message := range c.send {
		err := c.conn.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			log.Println("write:", err)
			return
		}
	}
}

func (c *Client) read(cr *ChatRoom) {
	defer func() {
		cr.unregister <- c
		c.conn.Close()
	}()
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			return
		}
		log.Println("incoming:", string(message))
		cr.broadcast <- []byte(c.nickname + ": " + string(message))
	}
}

func main() {
	chatRoom := NewChatRoom()
	go chatRoom.Run()
	r := gin.Default()
	r.GET("/ws", chatRoom.HandleWebSocket)
	r.Run(":8080")
	fmt.Println("Server started at localhost:8080")
}
