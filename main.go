package main

import (
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/fatih/color"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const binanceWSSURL = "wss://stream.binance.com:9443/ws"

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

// Alvos de preço
var priceTargets = map[string][]float64{
	"ICPUSDT":    {13.6, 13, 12.5},
	"LINKUSDT":   {21.7, 20.8, 18.44},
	"KSMUSDT":    {40, 37},
	"DIAUSDT":    {0.83, 0.73},
	"COTIUSDT":   {15, 14.4, 13.5},
	"SOLUSDT":    {210, 200, 190},
	"XLMUSDT":    {0.42, 0.36, 0.30},
	"KDAUSDT":    {1.377, 1.275, 1.15},
	"ALGOUSDT":   {0.42, 0.36, 0.30},
	"PENDLEUSDT": {6.1, 5.9, 5.6},
	"RNDRUSDT":   {9, 8, 7.2},
	"AAVEUSDT":   {220, 200, 183},
	"RAYUSDT":    {4.5, 4, 3.4},
	"JASMYUSDT":  {0.45},
	"GALAUSDT":   {0.50, 0.46, 0.40},
	"AVAXUSDT":   {47, 43},
	"SUPERUSDT":  {1.6, 1.5},
	"RSRUSDT":    {0.01800, 0.01500, 0.012},
}

func isWithinThreshold(price, target float64) bool {
	lowerBound := target * 0.99 // 1% abaixo do alvo
	upperBound := target * 1.01 // 1% acima do alvo
	return price >= lowerBound && price <= upperBound
}

func main() {
	// Configurar sinais para encerramento seguro
	done := make(chan os.Signal, 1)
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
		log.Fatalf("Erro ao conectar ao WebSocket da Binance: %v", err)
	}
	defer conn.Close()

	log.Println("Conectado ao WebSocket da Binance.")

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
						if isWithinThreshold(currentPrice, target) {
							// Texto em verde com fundo preto
							green := color.New(color.FgGreen, color.BgBlack).SprintFunc()
							log.Printf(green("[ALERTA] %s atingiu preço de $%f, próximo do alvo $%f"),
								trade.Symbol, currentPrice, target)

						}
					}
				} else {
					log.Printf("[%s] Preço atual: $%.2f", trade.Symbol, currentPrice)
				}
			}
		}
	}()

	<-done
	log.Println("Encerrando...")
}
