package game;

import (
	"time"

	"slices"
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
	EVENT_PLAYER_WON   = "PlayerWon";
	EVENT_PLAYER_LOST  = "PlayerLost";
);

type Bank interface {
	IncreaseBalance(string, string, decimal.Decimal) (decimal.Decimal, error);
	DecreaseBalance(string, string, decimal.Decimal) (decimal.Decimal, error);
	GetBalance(string, string) (decimal.Decimal, error);
};

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
	bank Bank;
	startTime time.Time;
	endTime time.Time;
	duration time.Duration;
};

func NewGame(io *socket.Server, db *sql.DB, bank Bank) (*Game, error) {
	gameId, err := uuid.NewV7();

	if err != nil {
		return nil, err;
	}

	return &Game{
		id: gameId,
		io: io,
		db: db,
		bank: bank,
		observers: make(map[socket.SocketId]*Observer),
		players: make([]*Player, 0),
	}, nil;
}

func (game *Game) createNewGame() {
	randInt, err := rand.Int(rand.Reader, big.NewInt(10));

	if err != nil {
		return;
	}

	gameId, err := uuid.NewV7();

	if err != nil {
		return;
	}

	game.id = gameId;
	game.state = GAMESTATE_WAITING;

	untilStart := time.Second * WAIT_TIME_SECS;
	game.startTime = time.Now().Add(untilStart);
	game.duration = time.Duration(time.Second * time.Duration(randInt.Int64()));
	game.endTime = game.startTime.Add(game.duration);

	time.AfterFunc(untilStart, game.handleGameStart);
	time.AfterFunc(untilStart + game.duration, game.handleGameCrash);

	slog.Info(
		"Created new game",
		"game",
		game.id,
		"startTime",
		game.startTime,
		"endTime",
		game.endTime,
	);

	game.Emit(EVENT_GAME_WAITING, map[string]any{
		"startTime": game.startTime.Unix(),
	});
}

func (game *Game) handleGameStart() {
	slog.Info("Preparing to start game...", "game", game.id);

	if len(game.observers) == 0 {
		slog.Info("No observers; not starting.");
		game.state = GAMESTATE_STOPPED;
		return;
	}

	slog.Info("Starting game...", "game", game.id, "duration", game.duration);

	game.state = GAMESTATE_RUNNING;

	game.Emit(EVENT_GAME_RUNNING, map[string]any{
		"startTime": game.startTime.Unix(),
	});
}

func (game *Game) handleGameCrash() {
	slog.Info("Crashing game...", "game", game.id);

	game.state = GAMESTATE_CRASHED;

	for i := range(game.players) {
		game.Emit(EVENT_PLAYER_LOST, map[string]any{
			"wallet": game.players[i].wallet,
		});
	}

	slog.Info("Entering game wait state...");

	game.players = nil;
	game.players = append(game.players, game.waiting...);
	game.waiting = nil;

	game.createNewGame();

	game.Emit(EVENT_GAME_CRASHED);
}

func (game *Game) HandlePlaceBet(
	client *socket.Socket,
	wallet string,
	currency string,
	betAmount decimal.Decimal,
	autoCashOut decimal.Decimal,
) error {
	player := Player{
		wallet: wallet,
		betAmount: betAmount,
		autoCashOut: autoCashOut,
		clientId: client.Id(),
	};

	for i := range(game.players) {
		if game.players[i].wallet == wallet {
			slog.Warn("Player already joined game");
			return errors.New("Player already joined game");
		}
	}

	_, err := game.bank.DecreaseBalance(wallet, currency, betAmount)

	if err != nil {
		slog.Warn("Failed to reduce user balance", "err", err);
		return err;
	}

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

/**
 * Temporary hack until I can figure out why io.Emit()
 * isn't working.
 */
func (game *Game) Emit(ev string, params ...any) {
	for _, observer := range game.observers {
		observer.socket.Emit(ev, params...);
	}
}
