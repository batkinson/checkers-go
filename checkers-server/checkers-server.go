package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"github.com/batkinson/checkers-go/checkers"
	"log"
	"math/rand"
	"net"
	"strconv"
	"strings"
)

type ClientMessage struct {
	Client  *Client
	Cmd     string
	CmdArgs []string
}

func main() {
	lnr, err := net.Listen("tcp", ":5000")
	if err != nil {
		log.Fatal(err)
	}
	serverMessages := make(chan ClientMessage, 4096)
	go serviceMessages((<-chan ClientMessage)(serverMessages))
	for {
		conn, err := lnr.Accept()
		if err != nil {
			log.Println(err)
			continue
		}
		go serviceConnection(conn, chan<- ClientMessage(serverMessages))
	}
}

type Game struct {
	Id         string
	Players    map[checkers.Player]*Client
	Spectators []*Client
	GameState  *checkers.Game
}

func (game *Game) SeatsFilled() bool {
	return len(game.OpenSeats()) == 0
}

func (game *Game) OpenSeats() []checkers.Player {
	openSeats := make([]checkers.Player, len(checkers.Players)-len(game.Players))
	i := 0
	for _, player := range checkers.Players {
		_, seatFilled := game.Players[player]
		if !seatFilled {
			openSeats[i] = player
		}
	}
	return openSeats
}

func (game *Game) TurnIs(client *Client) bool {
	for p, c := range game.Players {
		if c == client && game.GameState.TurnIs(p) {
			return true
		}
	}
	return false
}

func (game *Game) HasWinner() bool {
	return game.GameState.Winner() != checkers.NO_PLAYER
}

func (game *Game) Winner() string {
	return game.GameState.Winner().Color
}

func (game *Game) Broadcast(message string, excluded ...*Client) {
	isExcluded := make(map[*Client]bool)
	for _, client := range excluded {
		isExcluded[client] = true
	}
	for _, player := range game.Players {
		if !isExcluded[player] {
			player.Messages <- message
		}
	}
	for _, spectator := range game.Spectators {
		if !isExcluded[spectator] {
			spectator.Messages <- message
		}
	}
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func generateGameId(idLength int) string {
	b := make([]rune, idLength)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

var Games = make(map[string]*Game)
var Players = make(map[*Client]*Game)
var Spectators = make(map[*Client]*Game)

func NewGame() *Game {
	game := &Game{
		generateGameId(16),
		make(map[checkers.Player]*Client),
		make([]*Client, 0, 8),
		checkers.New(),
	}
	return game
}

func (game *Game) NeedsPlayer() bool {
	return !game.SeatsFilled() && !game.HasWinner()
}

func (game *Game) CanSpectate() bool {
	return game.SeatsFilled() && !game.HasWinner() && len(game.Spectators) < cap(game.Spectators)
}

func (game *Game) IsSpectator(client *Client) bool {
	for _, spectator := range game.Spectators {
		if spectator == client {
			return true
		}
	}
	return false
}

func (game *Game) Turn() string {
	if game.SeatsFilled() {
		return game.GameState.Turn.Color
	}
	return "waiting"
}

type Client struct {
	Conn     net.Conn
	Messages chan string
	Closing  chan bool
}

func NewClient(c net.Conn) *Client {
	messages := make(chan string, 16)
	closing := make(chan bool)
	return &Client{
		c,
		messages,
		closing,
	}
}

func (client *Client) IsSpectator() bool {
	if _, isSpectator := Spectators[client]; isSpectator {
		return true
	}
	return false
}

func (client *Client) IsPlayer() bool {
	if _, isPlayer := Players[client]; isPlayer {
		return true
	}
	return false
}

func (client *Client) IsInGame() bool {
	return client.IsSpectator() || client.IsPlayer()
}

func (client *Client) Close() {
	client.Closing <- true
	fmt.Println("closing", client.Conn.RemoteAddr())
	client.Conn.Close()
}

func (client *Client) serviceResponses() {
	for {
		select {
		case message := <-client.Messages:
			_, err := client.Conn.Write([]byte(message + "\r\n"))
			if err != nil {
				break
			}
		case closing := <-client.Closing:
			if closing {
				fmt.Println("shutdown", client.Conn.RemoteAddr())
				break
			}
		}
	}
}

func newGame(client *Client, args ...string) error {
	if len(args) > 0 {
		return errors.New("unsupported arguments")
	}
	if _, isPlaying := Players[client]; isPlaying {
		return errors.New("already in game")
	}
	game := NewGame()
	Games[game.Id] = game
	return joinGame(client, game.Id)
}

func listGames(client *Client, args ...string) error {
	spectate := len(args) == 1 && args[0] == "SPECTATE"
	if !spectate && len(args) > 0 {
		return errors.New("unsupported arguments")
	}
	var gameIds bytes.Buffer
	for gameId, game := range Games {
		if (spectate && game.CanSpectate() && !game.IsSpectator(client)) || (!spectate && game != Players[client] && game.NeedsPlayer()) {
			if gameIds.Len() > 0 {
				gameIds.WriteString(" ")
			}
			gameIds.WriteString(gameId)
		}
	}
	if spectate {
		client.Messages <- fmt.Sprintf("STATUS LIST SPECTATE %v", gameIds.String())
	} else {
		client.Messages <- fmt.Sprintf("STATUS LIST %v", gameIds.String())
	}
	return nil
}

func joinGame(client *Client, args ...string) (err error) {
	if len(args) != 1 {
		return errors.New("expected single game id")
	}
	gameId := args[0]
	if game, gameExists := Games[gameId]; gameExists {
		if game.SeatsFilled() {
			return errors.New("game is full")
		}
		assignedPlayer := game.OpenSeats()[0]
		if client.IsInGame() {
			leaveGame(client)
		}
		fmt.Println("joining", client.Conn.RemoteAddr(), game.Id, assignedPlayer.Color)
		game.Players[assignedPlayer] = client
		Players[client] = game
		client.Messages <- fmt.Sprintf("STATUS GAME_ID %v", gameId)
		client.Messages <- fmt.Sprintf("STATUS BOARD %v", game.GameState)
		client.Messages <- fmt.Sprintf("STATUS YOU_ARE %v", assignedPlayer.Color)
		game.Broadcast(fmt.Sprintf("STATUS JOINED %v", assignedPlayer.Color), client)
		game.Broadcast(fmt.Sprintf("STATUS TURN %v", game.Turn()))
	} else {
		err = errors.New("game " + gameId + " does not exist")
	}
	return err
}

func spectateGame(client *Client, args ...string) (err error) {
	if len(args) != 1 {
		return errors.New("expected single game id")
	}
	gameId := args[0]
	if game, gameExists := Games[gameId]; gameExists {
		if !game.CanSpectate() {
			return errors.New("game is not available for spectating")
		}
		if client.IsInGame() {
			leaveGame(client)
		}
		fmt.Println("spectate", client.Conn.RemoteAddr(), game.Id)
		Spectators[client] = game
		game.Spectators = append(game.Spectators, client)
		client.Messages <- fmt.Sprintf("STATUS GAME_ID %v", gameId)
		client.Messages <- fmt.Sprintf("STATUS BOARD %v", game.GameState)
		client.Messages <- fmt.Sprintf("STATUS TURN %v", game.Turn())
	} else {
		err = errors.New("game " + gameId + " does not exist")
	}
	return err
}

func leaveGame(client *Client, args ...string) (err error) {
	if len(args) > 0 {
		return errors.New("unsupported arguments")
	}
	if game, isPlaying := Players[client]; isPlaying {
		fmt.Println("leaving", client.Conn.RemoteAddr(), game.Id)
		delete(Players, client)
		for p, c := range game.Players {
			if c == client {
				delete(game.Players, p)
				game.Broadcast(fmt.Sprintf("STATUS LEFT %v", p.Color))
				game.Broadcast(fmt.Sprintf("STATUS TURN %v", game.Turn()))
				return err
			}
		}
		err = errors.New("failed to locate player in game")
	} else if game, isSpectating := Spectators[client]; isSpectating {
		fmt.Println("leaving", client.Conn.RemoteAddr(), game.Id)
		delete(Spectators, client)
		for i, c := range game.Spectators {
			if c == client {
				finalIndex := len(game.Spectators) - 1
				game.Spectators[i] = game.Spectators[finalIndex]
				game.Spectators[finalIndex] = c
				game.Spectators = game.Spectators[:finalIndex]
			}
		}
	} else {
		err = errors.New("not in game")
	}
	return err
}

func quit(client *Client, args ...string) error {
	if len(args) > 0 {
		return errors.New("unsupported arguments")
	}
	if client.IsInGame() {
		return leaveGame(client)
	}
	return nil
}

func createPos(coords []string) (src, dst checkers.Pos, err error) {
	if len(coords) != 4 {
		return checkers.NO_POS, checkers.NO_POS, errors.New("invalid positions, expected SRCX SRCY DSTX DSTY")
	}
	converted := make([]int, len(coords))
	for i, val := range coords {
		parsed, badVal := strconv.ParseInt(val, 0, 0)
		if badVal != nil {
			return checkers.NO_POS, checkers.NO_POS, badVal
		}
		converted[i] = int(parsed)
		i += 1
	}
	return checkers.Pos{converted[0], converted[1]}, checkers.Pos{converted[2], converted[3]}, err
}

func move(client *Client, args ...string) (err error) {
	game, isPlaying := Players[client]
	if isPlaying {
		src, dst, posErr := createPos(args)
		if posErr != nil {
			return posErr
		}
		if game.TurnIs(client) {
			wasKing := game.GameState.Pieces[src].King
			cap, mvErr := game.GameState.Move(src, dst)
			if mvErr != nil {
				err = mvErr
			} else {
				game.Broadcast(fmt.Sprintf("STATUS MOVED %v %v %v %v", src.X, src.Y, dst.X, dst.Y))
				if cap != checkers.NO_POS {
					game.Broadcast(fmt.Sprintf("STATUS CAPTURED %v %v", cap.X, cap.Y))
				}
				if !wasKing && game.GameState.Pieces[dst].King {
					game.Broadcast(fmt.Sprintf("STATUS KING %v %v", dst.X, dst.Y))
				}
				if game.HasWinner() {
					game.Broadcast(fmt.Sprintf("STATUS WINNER %v", game.Winner()))
				}
				game.Broadcast(fmt.Sprintf("STATUS TURN %v", game.Turn()))
			}
		} else {
			err = errors.New("not your turn")
		}
	} else {
		err = errors.New("not playing game")
	}
	return err
}

func boardStatus(client *Client, args ...string) (err error) {
	if len(args) > 0 {
		return errors.New("unsupported arguments")
	}
	game, playerInGame := Players[client]
	if playerInGame {
		client.Messages <- fmt.Sprintf("STATUS BOARD %v", game.GameState)
	} else {
		err = errors.New("not playing game")
	}
	return err
}

func turnStatus(client *Client, args ...string) (err error) {
	if len(args) > 0 {
		return errors.New("unsupported argments")
	}
	game, playerInGame := Players[client]
	if playerInGame {
		client.Messages <- fmt.Sprintf("STATUS TURN %v", game.Turn())
	} else {
		err = errors.New("not playing game")
	}
	return err
}

func serviceMessages(messages <-chan ClientMessage) {
	for {
		var err error
		select {
		case message := <-messages:
			err = serviceMessage(message)
			if err != nil {
				message.Client.Messages <- fmt.Sprintf("ERROR %v", err)
			} else {
				message.Client.Messages <- "OK"
			}
		}
	}
}

type Command func(*Client, ...string) error

var supportedCommands = map[string]Command{
	"NEW":      newGame,
	"LIST":     listGames,
	"JOIN":     joinGame,
	"LEAVE":    leaveGame,
	"MOVE":     move,
	"BOARD":    boardStatus,
	"TURN":     turnStatus,
	"SPECTATE": spectateGame,
	"QUIT":     quit,
}

func serviceMessage(message ClientMessage) (err error) {
	cmd, args := message.Cmd, message.CmdArgs
	if executor, cmdSupported := supportedCommands[cmd]; cmdSupported {
		err = executor(message.Client, args...)
	} else {
		err = errors.New("invalid command")
	}
	return err
}

func serviceConnection(c net.Conn, messages chan<- ClientMessage) {
	fmt.Println("connect", c.RemoteAddr())
	client := NewClient(c)
	defer client.Close()
	go client.serviceResponses()
	lines := bufio.NewReader(c)
	for {
		line, err := lines.ReadString(byte('\n'))
		if err != nil {
			break
		}
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 1 {
			break
		}
		cmd, args := fields[0], fields[1:]
		messages <- ClientMessage{client, cmd, args}
		if cmd == "QUIT" {
			break
		}
	}
}
