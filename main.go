package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/go-resty/resty/v2"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/joho/godotenv"
)

func init() {
	if err := godotenv.Load(); err != nil {
		fmt.Println("No .env file found")
	}
}

func main() {
	BOT_TOKEN := os.Getenv("TG_BOT_TOKEN")

	bot, err := tgbotapi.NewBotAPI(BOT_TOKEN)
	if err != nil {
		log.Panic(err)
	}
	bot.Debug = true

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)
	for update := range updates {
		if update.Message != nil && update.Message.Chat.Type == "private" {
			userMsg := update.Message.Text

			projectID, mrIID, err := parseUrlMr(userMsg)
			if err != nil {
				log.Println(err)
			}

			diffsStr, err := getChangesFromMR(projectID, mrIID)
			if err != nil {
				log.Println(err)
			}

			aiReply, err := askAi(diffsStr)
			if err != nil {
				log.Println(err)
			}

			msg := tgbotapi.NewMessage(update.Message.Chat.ID, aiReply)
			msg.ReplyToMessageID = update.Message.MessageID
			msg.ParseMode = "Markdown"

			if _, err := bot.Send(msg); err != nil {
				log.Println("Ошибка отправки:", err)
			}
		}
	}
}

// Merge Requests
func parseUrlMr(urlArg string) (projectID string, mrIID int, err error) {
	GITLAB_DOMAIN := os.Getenv("GITLAB_DOMAIN")

	spliter := "/-/merge_requests/"
	url_MR := strings.Split(urlArg, spliter)

	if len(url_MR) < 2 {
		return "", -1, fmt.Errorf("не валидная ссылка для МР: %v", err)
	}

	mrID := strings.Split(url_MR[1], "/")[0]
	pathToProject := strings.Split(url_MR[0], fmt.Sprintf("https://%v/", GITLAB_DOMAIN))[1]

	mrID_Num, err := strconv.Atoi(mrID)
	if err != nil {
		return "", -1, fmt.Errorf("не удалось конвертировать mr_id_num в число: %v", err)
	}

	return pathToProject, mrID_Num, nil
}

func getChangesFromMR(projectID string, mrIID int) (diffsChanges string, err error) {
	GITLAB_TOKEN := os.Getenv("GITLAB_TOKEN")
	GITLAB_DOMAIN := os.Getenv("GITLAB_DOMAIN")
	BASE_URL_API := fmt.Sprintf("https://%s/api/v4", GITLAB_DOMAIN)

	git, err := gitlab.NewClient(GITLAB_TOKEN, gitlab.WithBaseURL(BASE_URL_API))

	if err != nil {
		return "", fmt.Errorf("ошибка инициализации resty клиента: %v", err)
	}

	mr, _, err := git.MergeRequests.ListMergeRequestDiffs(projectID, mrIID, nil)

	if err != nil {
		return "", fmt.Errorf("ошибка получения изменений в мр: %v", err)

	}

	var allChanges string
	for _, change := range mr {
		allChanges = allChanges + fmt.Sprintf("Файл: %s\n Изменения:\n%s\n", change.NewPath, change.Diff)
	}

	return allChanges, nil

}

func askAi(body string) (string, error) {
	type Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	type Request struct {
		Model    string    `json:"model,omitempty"`
		Messages []Message `json:"messages,omitempty"`
	}

	type Response struct {
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	AI_TOKEN := os.Getenv("AI_TOKEN")
	AI_URL := os.Getenv("AI_URL")
	AI_MODEL := os.Getenv("AI_MODEL")

	client := resty.New()

	request := Request{
		Model: AI_MODEL,
		Messages: []Message{
			{
				Role:    "system",
				Content: "Ты — эксперт по ревью кода. Сначала дай краткое общее представление о том, какие изменения произошли в коде. Далее дай представление по каждому файлу, какие изменения произошли там [Название файла][Путь к файлу][Код изменений]. Далее проверь внесенные изменения на: 1. Читаемость кода. 2. Нет ли повторяющегося кода, 3. соответствует ли код лучшим практикам написания кода. В конце заключение",
			},
			{
				Role:    "user",
				Content: body,
			},
		},
	}

	var response Response

	_, err := client.
		R().
		SetHeader("Content-Type", "application/json").
		SetHeader("Accept", "application/json").
		SetHeader("Authorization", fmt.Sprintf("Bearer %s", AI_TOKEN)).
		SetBody(request).
		SetResult(&response).
		Post(AI_URL)

	if err != nil {
		return "", fmt.Errorf("err: %v", err)
	}

	if len(response.Choices) > 0 {
		return response.Choices[0].Message.Content, nil
	}

	return "", fmt.Errorf("no choices in response from AI")
}
