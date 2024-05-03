package game;

import (
	"time"

	"errors"

	"math/big"
	"crypto/rand"

	"log/slog"

	"database/sql"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/zishang520/socket.io/v2/socket"
);

const WAIT_TIME_SECS = 5;

const (
	GAMESTATE_STOPPED = iota;
	GAMESTATE_WAITING = iota;
	GAMESTATE_RUNNING = iota;
	GAMESTATE_CRASHED = iota;
	GAMESTATE_INVALID = iota;
);

const (
	EVENT_GAME_WAITING = "GameWaiting";
	EVENT_GAME_RUNNING = "GameRunning";
	EVENT_GAME_CRASHED = "GameCrashed";
	EVENT_USER_WON     = "UserWon";
	EVENT_USER_LOST    = "UserLost"
);

type CashOut struct {
	absTime time.Time;
	duration time.Duration;
	multiplier decimal.Decimal;
	cashedOut bool;
	auto bool;
	payout decimal.Decimal
};

type Player struct {
	betAmount decimal.Decimal;
	autoCashOut decimal.Decimal;
	cashOut CashOut;
	wallet string;
	clientId socket.SocketId;
};

type Observer struct {
	wallet string;
	socket *socket.Socket;
};

type Game struct {
	id uuid.UUID;
	state uint;
	players []*Player;
	waiting []*Player;
	observers map[socket.SocketId]*Observer;
	started time.Time;
	io *socket.Server;
	db *sql.DB;
	startTime time.Time;
	endTime time.Time;
};

func NewGame(io *socket.Server, db *sql.DB) (Game, error) {
	gameId, err := uuid.NewV7();

	if err != nil {
		return Game{}, err;
	}

	return Game{
		id: gameId,
		io: io,
		db: db,
		observers: make(map[socket.SocketId]*Observer),
		players: make([]*Player, 0),
	}, nil;
}

func (game *Game) createNewGame() {
	randInt, err := rand.Int(rand.Reader, big.NewInt(60));

	if err != nil {
		return;
	}

	gameId, err := uuid.NewV7();

	if err != nil {
		return;
	}

	game.id = gameId;
	game.state = GAMESTATE_WAITING;
	time.AfterFunc(time.Second * WAIT_TIME_SECS, game.handleGameStart);
	game.startTime = time.Now().Add(time.Second * WAIT_TIME_SECS);

	endTime := time.Duration(time.Second * time.Duration(randInt.Int64()));
	time.AfterFunc(endTime, game.handleGameCrash);
	game.endTime = time.Now().Add(endTime);

	slog.Info(
		"Created new game",
		"game",
		game.id,
		"startTime",
		game.startTime,
		"endTime",
		game.endTime,
	);

	for _, observer := range game.observers {
		observer.socket.Emit(EVENT_GAME_WAITING, map[string]any{
			"startTime": game.startTime.Unix(),
		});
	}
}

func (game *Game) handleGameStart() {
	slog.Info("Preparing to start game...", "game", game.id);

	if len(game.observers) == 0 {
		slog.Info("No observers; not starting.");
		game.state = GAMESTATE_STOPPED;
		return;
	}

	slog.Info("Starting game...");

	game.state = GAMESTATE_RUNNING;

	game.io.Emit(EVENT_GAME_RUNNING, map[string]any{
		"startTime": game.startTime.Unix(),
	});
}

func (game *Game) handleGameCrash() {
	slog.Info("Crashing game...", "game", game.id);

	game.state = GAMESTATE_CRASHED;

	for i := range(game.players) {
		observer, found := game.observers[game.players[i].clientId];

		if found {
			observer.socket.Emit(EVENT_USER_LOST);
		}
	}

	slog.Info("Entering game wait state...");

	game.players = nil;
	game.players = append(game.players, game.waiting...);
	game.waiting = nil;

	game.createNewGame();

	game.io.Emit(EVENT_GAME_CRASHED);
}

func (game *Game) HandlePlaceBet(client *socket.Socket) error {
	betAmount, err := decimal.NewFromString("1.4");

	if err != nil {
		return err;
	}

	autoCashOut, err := decimal.NewFromString("2.0");

	player := Player{
		betAmount: betAmount,
		autoCashOut: autoCashOut,
		clientId: client.Id(),
	};

	if game.state == GAMESTATE_WAITING {
		game.players = append(game.players, &player);
	} else if (game.state == GAMESTATE_RUNNING) {
		game.waiting = append(game.waiting, &player);
	} else {
		return errors.New("Unable to join game");
	}

	return nil;
}

func (game *Game) HandleCancelBet(client *socket.Socket) error {
	return errors.New("Unimplemented");
}

func (game *Game) HandleCashOut(client *socket.Socket) error {
	return errors.New("Unimplemented");
}

func (game *Game) HandleConnect(client *socket.Socket, wallet string) {
	_, exists := game.observers[client.Id()];

	if exists {
		return;
	}

	observer := Observer{
		wallet: wallet,
		socket: client,
	};

	game.observers[client.Id()] = &observer;

	if game.state == GAMESTATE_STOPPED {
		slog.Info("Entering game wait state...");
		game.createNewGame();

		return;
	}

	if game.state == GAMESTATE_WAITING {
		observer.socket.Emit(EVENT_GAME_WAITING, map[string]any{
			"startTime": game.startTime.Unix(),
		});

		return;
	}
}

func (game *Game) HandleDisconnect(client *socket.Socket) {
	_, exists := game.observers[client.Id()];

	if !exists {
		return;
	}

	delete(game.observers, client.Id());
}
