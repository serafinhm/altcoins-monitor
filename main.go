package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fatih/color"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const binanceWSSURL = "wss://stream.binance.com:9443/ws"

var (
	bot *tgbotapi.BotAPI
)

type TradeTelegramMessage struct {
	ChatID int64
	Symbol string
	Price  float64
	Target float64
}

// TradeMessage representa a estrutura de dados recebida para preços.
type TradeMessage struct {
	Event         string `json:"e"` // Evento (ex: "trade")
	EventTime     int64  `json:"E"` // Timestamp do evento
	Symbol        string `json:"s"` // Símbolo do ativo
	TradeID       int64  `json:"t"` // ID da transação
	Price         string `json:"p"` // Preço da transação
	Quantity      string `json:"q"` // Quantidade da transação
	Timestamp     int64  `json:"T"` // Timestamp da transação
	IsMarketMaker bool   `json:"m"` // Flag se é Market Maker
	Ignore        bool   `json:"M"` // Campo ignorado
}

var chatIds = []int64{
	-1002314879454,
	6753790669,
	6717764833,
}

// Alvos de preço
var priceTargets = map[string][]float64{
	"LINKUSDT":   {21.7, 20.8, 18.44, 25.00},
	"KSMUSDT":    {40, 37},
	"COTIUSDT":   {15.4, 14.4, 12.7},
	"SOLUSDT":    {210, 200, 190},
	"XLMUSDT":    {0.42, 0.36, 0.30},
	"ALGOUSDT":   {0.42, 0.36, 0.30},
	"PENDLEUSDT": {5.9, 5.6, 5.4},
	"RNDRUSDT":   {9, 8, 7.2},
	"RAYUSDT":    {4.5, 4, 3.4},
	"JASMYUSDT":  {0.045},
	"GALAUSDT":   {0.50, 0.46, 0.40},
	"AVAXUSDT":   {47, 43},
	"KDAUSDT":    {1.31, 1.15, 1},
	"ICPUSDT":    {13.5, 13, 12.3},
	"DIAUSDT":    {0.88, 0.84, 0.80},
	"SUPERUSDT":  {1.6, 1.5},
	"RSRUSDT":    {0.01800, 0.01500, 0.012},
	"TAOUSDT":    {680, 655, 635},
	"ONDOUSDT":   {1.45, 1.28, 1.11},
	"ZILUSDT":    {0.285, 0.253, 0.2218},
	"LITUSDT":    {1.1, 1.0, 0.8},
	"TIAUSDT":    {7.65, 6.8},
}

func isWithinThreshold(price, target float64) bool {
	lowerBound := target * 0.99 // 1% abaixo do alvo
	upperBound := target * 1.01 // 1% acima do alvo
	return price >= lowerBound && price <= upperBound
}

func botTelegram() {
	red := color.New(color.FgRed).SprintFunc()
	botAPI, err := tgbotapi.NewBotAPI("")
	if err != nil {
		log.Print(red("Erro ao conectar na api do telegram"))
	}
	bot = botAPI
}

func sendMessage(message TradeTelegramMessage) {
	msg := tgbotapi.NewMessage(message.ChatID, fmt.Sprintf("ALERTA: %s atingiu preço de $%.2f, próximo do alvo $%.2f", message.Symbol, message.Price, message.Target))
	_, err := bot.Send(msg)
	if err != nil {
		log.Printf("Erro ao enviar mensagem para o chat %d: %v", message.ChatID, err)
	}
}

func main() {
	// start bot do telegram
	botTelegram()

	// Configurar sinais para encerramento seguro
	done := make(chan os.Signal, 1)

	alertTimer := time.NewTimer(0)
	<-alertTimer.C

	alertEmitted := false

	var lastSymbol string

	red := color.New(color.FgRed).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()

	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	// Lista de ativos a serem monitorados
	assets := []string{}
	for asset := range priceTargets {
		assets = append(assets, strings.ToLower(asset)+"@trade")
	}

	// Combina as streams em uma única subscrição
	streams := strings.Join(assets, "/")

	// Conectar ao WebSocket da Binance
	conn, _, err := websocket.DefaultDialer.Dial(binanceWSSURL+"/"+streams, nil)
	if err != nil {
		log.Fatalf(red("Erro ao conectar ao WebSocket da Binance: %v"), err)
	}
	defer conn.Close()

	log.Println(yellow("Conectado ao WebSocket da Binance."))

	// Gerar um UUID para a requisição
	requestID := uuid.New().String()

	// Mensagem de subscrição
	subscribeMessage := map[string]interface{}{
		"method": "SUBSCRIBE",
		"params": assets,
		"id":     requestID,
	}

	if err := conn.WriteJSON(subscribeMessage); err != nil {
		log.Fatalf("Erro ao subscrever: %v", err)
	}
	log.Printf("Subscrito para ativos: %v", assets)

	// Goroutine para processar mensagens
	go func() {
		defer close(done)
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("Conexão fechada com código de status: %v", websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				} else {
					log.Printf("Erro ao ler mensagem: %v", err)
				}
				return
			}

			var trade TradeMessage
			if err := json.Unmarshal(message, &trade); err != nil {
				log.Printf("Erro ao processar mensagem: %v", err)
				panic(err)
			}

			if strings.TrimSpace(trade.Event) == "trade" {
				currentPrice, err := strconv.ParseFloat(trade.Price, 64)
				if err != nil {
					log.Printf("Erro ao converter preço: %v", err)
					continue
				}

				// Verificar alertas para o ativo
				if targets, ok := priceTargets[trade.Symbol]; ok {
					for _, target := range targets {
						if isWithinThreshold(currentPrice, target) && (!alertEmitted || lastSymbol != trade.Symbol) {

							// Marcar que o alerta foi emitido
							alertEmitted = true
							lastSymbol = trade.Symbol

							// Reiniciar o timer para permitir novos alertas após 1 minuto
							if alertTimer != nil {
								alertTimer.Stop()
							}
							alertTimer = time.AfterFunc(1*time.Minute, func() {
								alertEmitted = false
							})

							green := color.New(color.FgGreen, color.BgBlack).SprintFunc()
							// Enviar alerta para o Telegram
							sendTelegramChats(trade, currentPrice, target)
							log.Printf(green("[ALERTA] %s atingiu preço de $%f, próximo do alvo $%f"),
								trade.Symbol, currentPrice, target)

						}
					}
				} else {
					log.Printf("[%s] Preço atual: $%.2f", trade.Symbol, currentPrice)
				}
				// Verificar se o timer expirou para permitir novos alertas
				select {
				case <-alertTimer.C:
					alertEmitted = false
				default:
				}
			}
		}
	}()

	<-done
	log.Println("Encerrando...")
}

func sendTelegramChats(trade TradeMessage, currentPrice float64, target float64) {
	for _, chatID := range chatIds {
		message := TradeTelegramMessage{
			ChatID: chatID,
			Symbol: trade.Symbol,
			Price:  currentPrice,
			Target: target,
		}
		sendMessage(message)
	}
}
