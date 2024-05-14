package game;

import (
	"time"
	"math"
	"encoding/json"

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
	IncreaseBalance(
		string,
		string,
		decimal.Decimal,
		string,
		uuid.UUID,
	) (decimal.Decimal, error);

	DecreaseBalance(
		string,
		string,
		decimal.Decimal,
		string,
		uuid.UUID,
	) (decimal.Decimal, error);

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
	currency string;
	autoCashOut decimal.Decimal;
	cashOut CashOut;
	wallet string;
	clientId socket.SocketId;
	timeOut *time.Timer;
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
	io *socket.Server;
	db *sql.DB;
	bank Bank;
	startTime time.Time;
	endTime time.Time;
	duration time.Duration;
};

type CrashedGame struct {
	startTime time.Time;
	duration time.Duration;
	multiplier decimal.Decimal;
}

func (p *Player) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{
		"betAmount"  : p.betAmount.String(),
		"currency"   : p.currency,
		"autoCashOut": p.currency,
		"wallet"     : p.wallet,
	});
}

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
		waiting: make([]*Player, 0),
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

	makeCallback := func(player *Player) func() {
		return func() {
			slog.Info("Auto cashing out", "wallet", player.wallet);
			game.handleCashOut(player.wallet, true);
		}
	};

	for i := range(game.players) {
		if !game.players[i].autoCashOut.Equal(decimal.Zero) {
			autoCashOut, _ := game.players[i].autoCashOut.Float64();
			timeOut := time.Duration(float64(time.Millisecond) * math.Log(autoCashOut) / 6E-5);
			game.players[i].timeOut = time.AfterFunc(timeOut, makeCallback(game.players[i]));
		}
	}

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

	err := game.saveRecord();

	if err != nil {
		slog.Error("Error saving game record", "err", err);
	}

	slog.Info("Entering game wait state...");

	game.clearTimers();
	game.commitWaiting();

	game.Emit(EVENT_GAME_CRASHED);

	time.AfterFunc(WAIT_TIME_SECS * time.Second, game.createNewGame);
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
		currency: currency,
		autoCashOut: autoCashOut,
		clientId: client.Id(),
	};

	for i := range(game.players) {
		if game.players[i].wallet == wallet {
			slog.Warn("Player already joined game");
			return errors.New("player already joined game");
		}
	}

	_, err := game.bank.DecreaseBalance(
		wallet,
		currency,
		betAmount,
		"Bet placed",
		game.id,
	);

	if err != nil {
		slog.Warn("Failed to reduce user balance", "err", err);
		return err;
	}

	if game.state == GAMESTATE_WAITING {
		game.players = append(game.players, &player);
	} else if (game.state == GAMESTATE_RUNNING) {
		game.waiting = append(game.waiting, &player);
	} else {
		return errors.New("unable to join game");
	}

	game.Emit("BetList", map[string]any{
		"players": game.players,
		"waiting": game.waiting,
	});

	return nil;
}

func (game *Game) HandleCancelBet(wallet string) error {
	playerIndex := slices.IndexFunc(game.players, func(p *Player) bool {
		return p.wallet == wallet;
	});

	if playerIndex == -1 {
		return errors.New("cancel bet denied - player not in list");
	}

	game.players = slices.Delete(game.players, playerIndex, playerIndex + 1);

	return nil;
}

func (game *Game) HandleCashOut(wallet string) error {
	return game.handleCashOut(wallet, false);
}

func (game *Game) handleCashOut(wallet string, auto bool) error {
	if game.state != GAMESTATE_RUNNING {
		return errors.New("cash out denied - game finished or not running");
	}

	playerIndex := slices.IndexFunc(game.players, func(p *Player) bool {
		return p.wallet == wallet;
	});

	if playerIndex == -1 {
		return errors.New("cash out denied - player not in list");
	}

	player := game.players[playerIndex];

	if player.cashOut.cashedOut {
		return errors.New("cash out denied - already cashed out");
	}

	timeNow := time.Now();
	duration := timeNow.Sub(game.startTime);

	payout, multiplier := game.calculatePayout(
		duration,
		player.betAmount,
	);

	slog.Info("Player cashed out", "wallet", player.wallet, "payout", payout, "currency", player.currency);

	player.cashOut = CashOut{
		absTime: timeNow,
		duration: duration,
		multiplier: multiplier,
		payout: payout,
		cashedOut: true,
		auto: auto,
	};

	var reason string;

	if (auto) {
		reason = "Auto cashout";
	} else {
		reason = "Cashout";
	}

	game.bank.IncreaseBalance(
		player.wallet,
		player.currency,
		payout,
		reason,
		game.id,
	);

	game.Emit("BetList", map[string]any{
		"players": game.players,
		"waiting": game.waiting,
	});

	return nil;
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

func (game *Game) clearTimers() {
	for i := range(game.players) {
		if game.players[i].timeOut != nil {
			game.players[i].timeOut.Stop();
			game.players[i].timeOut = nil;
		}
	}
}

func (game *Game) commitWaiting() {
	game.players = []*Player{};
	game.players = append(game.players, game.waiting...);
	game.waiting = []*Player{};
}

func (game *Game) calculatePayout(
	duration time.Duration,
	betAmount decimal.Decimal,
) (decimal.Decimal, decimal.Decimal) {
	durationMs := decimal.NewFromInt(duration.Milliseconds());
	coeff := decimal.NewFromFloat(6E-5);
	e := decimal.NewFromFloat(math.Exp(1));
	multiplier := e.Pow(coeff.Mul(durationMs)).Truncate(2);

	return betAmount.Mul(multiplier), multiplier;
}

func (game *Game) getRecentGames(limit int) ([]CrashedGame, error) {
	var games []CrashedGame;

	rows, err := game.db.Query(`
		SELECT startTime, (endTime - startTime) AS duration,
		multiplier
		FROM games
		ORDER BY created DESC
		LIMIT ?
	`, limit);

	for rows.Next() {
		var gameRow CrashedGame;

		rows.Scan(
			gameRow.startTime,
			gameRow.duration,
			gameRow.multiplier,
		);

		games = append(games, gameRow);
	}

	if err != nil {
		return nil, err;
	}

	return games, nil;
}

func (game *Game) saveRecord() (error) {
	winners := 0;
	players := len(game.players);

	for i := range(game.players) {
		if game.players[i].cashOut.cashedOut {
			winners++;
		}
	}

	_, err := game.db.Exec(`
		INSERT INTO games
		(id, startTime, endTime, playerCount, winnerCount)
		VALUES
		(?, ?, ?, ?, ?)
	`, game.id, game.startTime, game.endTime, players, winners);

	if err != nil {
		return err;
	}

	return nil;
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
