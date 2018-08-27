package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/keybase/go-keybase-chat-bot/kbchat"
	"github.com/spf13/viper"
)

type webhook struct {
	Token string
	Team  string
}

type config struct {
	KeybaseBin    string
	ListenAddress string
	Webhooks      []webhook
}

type webhookRequest struct {
	Text    string `json:"text"`
	Channel string `json:"channel,omitempty"`
}

type webhookPayload struct {
	Text    string
	Channel string
	Team    string
}

func parseRequest(request *http.Request) (webhookRequest, error) {
	var msg webhookRequest
	var err error

	contentType := request.Header.Get("Content-Type")
	if contentType == "application/json" {
		err = json.NewDecoder(request.Body).Decode(&msg)
	} else if contentType == "application/x-www-form-urlencoded" {
		err = request.ParseForm()
		if err == nil {
			err = json.NewDecoder(strings.NewReader(request.PostFormValue("payload"))).Decode(&msg)
		}
	} else {
		err = errors.New("Unknown content type")
	}

	return msg, err
}

func webhookHandler(webhooks []webhook, ch chan<- webhookPayload, writer http.ResponseWriter, request *http.Request) {
	vars := mux.Vars(request)
	token := vars["token"]

	for _, w := range webhooks {
		if w.Token == token {
			msg, err := parseRequest(request)
			if err != nil {
				log.Printf("Invalid request: %v", err)
				http.Error(writer, err.Error(), 400)
				return
			}
			payload := webhookPayload{Text: msg.Text, Channel: msg.Channel, Team: w.Team}
			if payload.Channel == "" {
				payload.Channel = "general"
			}
			log.Printf("Webhook payload: %+v\n", payload)
			ch <- payload
			return
		}
	}

	http.Error(writer, "Invalid token", 403)
}

func keybaseHandler(keybaseChat *kbchat.API, payloadChannel <-chan webhookPayload) {
	for payload := range payloadChannel {
		if err := keybaseChat.SendMessageByTeamName(payload.Team, payload.Text, &payload.Channel); err != nil {
			log.Fatal("Error sending message: ", err.Error())
		}
	}
}

func initConfig() {
	viper.SetDefault("KeybaseBin", "keybase")
	viper.SetDefault("ListenAddress", ":8080")

	viper.SetConfigName("config")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		log.Fatal("Error loading config file: ", err.Error())
	}
}

func main() {
	initConfig()

	var c config
	err := viper.Unmarshal(&c)
	if err != nil {
		log.Fatal("Error loading config file: ", err.Error())
	}

	keybaseChat, err := kbchat.Start(c.KeybaseBin)
	if err != nil {
		log.Fatal("Error creating API: ", err.Error())
	}
	log.Println("Keybase API user: ", keybaseChat.Username())

	payloadChannel := make(chan webhookPayload)
	go keybaseHandler(keybaseChat, payloadChannel)

	router := mux.NewRouter()
	router.HandleFunc("/hooks/{token}", func(writer http.ResponseWriter, request *http.Request) {
		webhookHandler(c.Webhooks, payloadChannel, writer, request)
	})
	http.Handle("/", router)

	log.Println("Listening on ", c.ListenAddress)
	log.Fatal(http.ListenAndServe(c.ListenAddress, nil))
}
