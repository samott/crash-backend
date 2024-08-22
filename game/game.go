package game

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math"
	"time"
	"sync"

	"errors"
	"slices"
	"strconv"

	"crypto/sha256"
	"crypto/rand"

	"database/sql"

	"cloud.google.com/go/logging"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/zishang520/socket.io/v2/socket"

	"github.com/samott/crash-backend/config"
);

var (
	ErrUserAlreadyJoined = errors.New("user already joined game")
	ErrWrongGameState = errors.New("action invalid for current game state")
	ErrUserNotWaiting = errors.New("user not in waiting list")
	ErrUserNotPlaying = errors.New("user not playing")
	ErrAlreadyCashedOut = errors.New("player already cashed out")
)

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

type Log = map[string]any;

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

	GetBalances(wallet string) (map[string]decimal.Decimal, error);
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
	hash string;
	state uint;
	players []*Player;
	waiting []*Player;
	observers map[socket.SocketId]*Observer;
	io *socket.Server;
	db *sql.DB;
	logger *logging.Logger;
	config *config.CrashConfig;
	bank Bank;
	startTime time.Time;
	endTime time.Time;
	duration time.Duration;
	lock *sync.Mutex;
};

type CrashedGame struct {
	id uuid.UUID;
	startTime time.Time;
	duration time.Duration;
	multiplier decimal.Decimal;
	players int;
	winners int;
}

func (p *Player) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"betAmount"  : p.betAmount.String(),
		"currency"   : p.currency,
		"autoCashOut": p.autoCashOut.StringFixed(2),
		"cashOut"    : p.cashOut.multiplier.StringFixed(2),
		"isCashedOut": p.cashOut.cashedOut,
		"wallet"     : p.wallet,
	});
}

func (g *CrashedGame) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"id"         : g.id.String(),
		"startTime"  : g.startTime.UnixMilli(),
		"duration"   : g.duration.Milliseconds(),
		"multiplier" : g.multiplier.StringFixed(2),
		"players"    : g.players,
		"winners"    : g.winners,
	});
}

func NewGame(
	io *socket.Server,
	db *sql.DB,
	config *config.CrashConfig,
	logger *logging.Logger,
	bank Bank,
) (*Game, error) {
	gameId, err := uuid.NewV7();

	if err != nil {
		return nil, err;
	}

	return &Game{
		id: gameId,
		io: io,
		db: db,
		config: config,
		logger: logger,
		bank: bank,
		observers: make(map[socket.SocketId]*Observer),
		players: make([]*Player, 0),
		waiting: make([]*Player, 0),
		lock: &sync.Mutex{},
	}, nil;
}

func (game *Game) GetConfig() (*config.CrashConfig) {
	return game.config;
}

func generateRandomSeed(length int) (string, error) {
	buffer := make([]byte, length);
	_, err := rand.Read(buffer);
	if err != nil {
		return "", err;
	}
	result := base64.URLEncoding.EncodeToString(buffer)[:length];
	return result, nil;
}

func generateGameHash(seed string) string {
	s := sha256.New();
	s.Write([]byte(seed))
	return  hex.EncodeToString(s.Sum(nil));
}

func hashToMultiplier(hash string) decimal.Decimal {
	h, _ := strconv.ParseUint(hash[0:13], 16, 64);
	e := math.Pow(2, 52);
	r := math.Floor((98 * e) / (e - float64(h)));
	m := math.Round(r) / 100;

	if (m < 1) {
		return decimal.NewFromInt(1);
	}

	return decimal.NewFromFloat(m).Round(2);
}

func multiplierToDuration(multiplier decimal.Decimal) (time.Duration, error) {
	r, err := multiplier.Ln(10);

	if err != nil {
		return time.Duration(0), err;
	}

	d := decimal.NewFromFloat(6e-5);
	r = r.Div(d);

	return time.Duration(r.IntPart() * int64(time.Millisecond)), nil;
}

func (game *Game) createNewGame() {
	seed, err := generateRandomSeed(64);

	if err != nil {
		return;
	}

	hash := generateGameHash(seed);
	multiplier := hashToMultiplier(hash);
	duration, err := multiplierToDuration(multiplier);

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
	game.hash = hash;
	game.startTime = time.Now().Add(untilStart);
	game.duration = duration;
	game.endTime = game.startTime.Add(game.duration);

	time.AfterFunc(untilStart, game.handleGameStart);

	game.logger.Log(logging.Entry{
		Payload: Log{
			"msg"      : "Created new game",
			"game"     : game.id,
			"startTime": game.startTime,
			"endTime"  : game.endTime,
		},
		Severity: logging.Info,
	});

	game.Emit(EVENT_GAME_WAITING, map[string]any{
		"startTime": game.startTime.UnixMilli(),
	});
}

func (game *Game) handleCreateNewGame() {
	game.lock.Lock();
	defer game.lock.Unlock();

	game.createNewGame();
}

func (game *Game) handleGameStart() {
	game.lock.Lock();
	defer game.lock.Unlock();

	game.logger.Log(logging.Entry{
		Payload: Log{
			"msg" : "Preparing to start game...",
			"game": game.id,
		},
		Severity: logging.Info,
	});

	if len(game.waiting) == 0 && len(game.observers) == 0 {
		game.logger.Log(logging.Entry{
			Payload: Log{
				"msg" : "No observers; not starting.",
				"game": game.id,
			},
			Severity: logging.Info,
		});

		game.state = GAMESTATE_STOPPED;
		return;
	}

	game.logger.Log(logging.Entry{
		Payload: Log{
			"msg"     : "Starting game...",
			"game"    : game.id,
			"duration": game.duration.Seconds(),
		},
		Severity: logging.Info,
	});

	game.state = GAMESTATE_RUNNING;

	game.commitWaiting();

	makeCallback := func(player *Player) func() {
		return func() {
			game.logger.Log(logging.Entry{
				Payload: Log{
					"msg"   : "Auto cashing out...",
					"game"  : game.id,
					"wallet": player.wallet,
				},
				Severity: logging.Info,
			});

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

	time.AfterFunc(game.duration, game.handleGameCrash);

	game.Emit(EVENT_GAME_RUNNING, map[string]any{
		"startTime": game.startTime.UnixMilli(),
	});
}

func (game *Game) handleGameCrash() {
	game.lock.Lock();
	defer game.lock.Unlock();

	game.logger.Log(logging.Entry{
		Payload: Log{
			"msg"   : "Crashing game...",
			"game"  : game.id,
		},
		Severity: logging.Info,
	});

	game.state = GAMESTATE_CRASHED;

	for i := range(game.players) {
		game.Emit(EVENT_PLAYER_LOST, map[string]any{
			"wallet": game.players[i].wallet,
		});
	}

	record, err := game.saveRecord();

	if err != nil {
		game.logger.Log(logging.Entry{
			Payload: Log{
				"msg"  : "Error saving game record.",
				"game" : game.id,
				"error": err,
			},
			Severity: logging.Error,
		});

		game.clearTimers();
		return;
	}

	game.logger.Log(logging.Entry{
		Payload: Log{
			"msg": "Entering game wait state...",
		},
		Severity: logging.Info,
	});

	game.clearTimers();

	game.Emit(EVENT_GAME_CRASHED, map[string]*CrashedGame{
		"game": record,
	});

	time.AfterFunc(WAIT_TIME_SECS * time.Second, game.handleCreateNewGame);
}

func (game *Game) HandlePlaceBet(
	client *socket.Socket,
	wallet string,
	currency string,
	betAmount decimal.Decimal,
	autoCashOut decimal.Decimal,
) error {
	game.lock.Lock();
	defer game.lock.Unlock();

	player := Player{
		wallet: wallet,
		betAmount: betAmount,
		currency: currency,
		autoCashOut: autoCashOut,
		clientId: client.Id(),
	};

	for i := range(game.waiting) {
		if game.waiting[i].wallet == wallet {
			game.logger.Log(logging.Entry{
				Payload: Log{
					"msg"   : "Player already joined waitlist...",
					"game"  : game.id,
					"wallet": wallet,
				},
				Severity: logging.Warning,
			});

			return ErrUserAlreadyJoined;
		}
	}

	bal, err := game.bank.GetBalance(
		wallet,
		currency,
	);

	if err != nil {
		game.logger.Log(logging.Entry{
			Payload: Log{
				"msg"   : "Failed to get user balance",
				"wallet": wallet,
				"error" : err,
			},
			Severity: logging.Warning,
		});

		return err;
	}

	if bal.LessThan(betAmount) {
		game.logger.Log(logging.Entry{
			Payload: Log{
				"msg"      : "Insufficient balance for operation",
				"wallet"   : wallet,
				"betAmount": betAmount,
				"balance"  : bal,
				"currency" : currency,
			},
			Severity: logging.Warning,
		});

		return err;
	}

	game.waiting = append(game.waiting, &player);

	game.emitBetList();

	return nil;
}

func (game *Game) HandleCancelBet(wallet string) error {
	game.lock.Lock();
	defer game.lock.Unlock();

	playerIndex := slices.IndexFunc(game.waiting, func(p *Player) bool {
		return p.wallet == wallet;
	});

	if playerIndex == -1 {
		return ErrUserNotWaiting;
	}

	game.waiting = slices.Delete(game.waiting, playerIndex, playerIndex + 1);

	game.emitBetList();

	return nil;
}

func (game *Game) HandleCashOut(wallet string) error {
	return game.handleCashOut(wallet, false);
}

func (game *Game) handleCashOut(wallet string, auto bool) error {
	game.lock.Lock();
	defer game.lock.Unlock();

	if game.state != GAMESTATE_RUNNING {
		return ErrWrongGameState;
	}

	playerIndex := slices.IndexFunc(game.players, func(p *Player) bool {
		return p.wallet == wallet;
	});

	if playerIndex == -1 {
		return ErrUserNotPlaying;
	}

	player := game.players[playerIndex];

	if player.cashOut.cashedOut {
		return ErrAlreadyCashedOut;
	}

	timeNow := time.Now();
	duration := timeNow.Sub(game.startTime);

	payout, multiplier := game.calculatePayout(
		duration,
		player.betAmount,
	);

	game.logger.Log(logging.Entry{
		Payload: Log{
			"msg"      : "Player cashed out",
			"game"     : game.id,
			"wallet"   : player.wallet,
			"payout"   : payout,
			"currency" : player.currency,
		},
		Severity: logging.Info,
	});

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

	newBalance, err := game.bank.IncreaseBalance(
		player.wallet,
		player.currency,
		payout,
		reason,
		game.id,
	);

	if err != nil {
		game.logger.Log(logging.Entry{
			Payload: Log{
				"msg"     : "Failed to credit win",
				"game"    : game.id,
				"wallet"  : player.wallet,
				"payout"  : payout,
				"currency": player.currency,
			},
			Severity: logging.Error,
		});
	}

	game.emitBalanceUpdate(player, newBalance);

	game.Emit(EVENT_PLAYER_WON, map[string]any{
		"wallet"    : player.wallet,
		"multiplier": multiplier,
	});

	return nil;
}

func (game *Game) HandleConnect(client *socket.Socket) {
	game.lock.Lock();
	defer game.lock.Unlock();

	_, exists := game.observers[client.Id()];

	if exists {
		return;
	}

	observer := Observer{
		wallet: "",
		socket: client,
	};

	game.observers[client.Id()] = &observer;

	if recentGames, err := game.getRecentGames(10); err == nil {
		observer.socket.Emit("RecentGameList", map[string]any{
			"games": recentGames,
		});
	}

	if game.state == GAMESTATE_STOPPED {
		game.logger.Log(logging.Entry{
			Payload: Log{
				"msg": "Entering game wait state...",
			},
			Severity: logging.Info,
		});

		game.createNewGame();

		return;
	}

	if game.state == GAMESTATE_WAITING {
		observer.socket.Emit(EVENT_GAME_WAITING, map[string]any{
			"startTime": game.startTime.UnixMilli(),
		});

		return;
	}
}

func (game *Game) HandleLogin(client *socket.Socket, wallet string) {
	game.lock.Lock();
	defer game.lock.Unlock();

	observer, exists := game.observers[client.Id()];

	if !exists {
		return;
	}

	observer.wallet = wallet;

	balances, err := game.bank.GetBalances(wallet);

	if err != nil {
		return;
	}

	observer.socket.Emit("InitBalances", map[string]map[string]decimal.Decimal{
		"balances" : balances,
	});
}

func (game *Game) HandleDisconnect(client *socket.Socket) {
	game.lock.Lock();
	defer game.lock.Unlock();

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

	for i := range(game.waiting) {
		newBalance, err := game.bank.DecreaseBalance(
			game.waiting[i].wallet,
			game.waiting[i].currency,
			game.waiting[i].betAmount,
			"Bet placed",
			game.id,
		);

		if err != nil {
			game.logger.Log(logging.Entry{
				Payload: Log{
					"msg"   : "Unable to take balance for user; removing from game...",
					"game"  : game.id,
					"wallet": game.waiting[i].wallet,
				},
				Severity: logging.Warning,
			});

			continue;
		}

		game.emitBalanceUpdate(game.waiting[i], newBalance);

		game.players = append(game.players, game.waiting[i]);
	}

	game.waiting = []*Player{};

	game.emitBetList();
}

func (game *Game) calculatePayout(
	duration time.Duration,
	betAmount decimal.Decimal,
) (decimal.Decimal, decimal.Decimal) {
	durationMs := decimal.NewFromInt(duration.Milliseconds());
	coeff := decimal.NewFromFloat(6E-5);
	e := decimal.NewFromFloat(math.Exp(1));
	multiplier := e.Pow(coeff.Mul(durationMs)).Round(2);

	return betAmount.Mul(multiplier), multiplier;
}

func (game *Game) calculateFinalMultiplier() (decimal.Decimal) {
	duration := game.endTime.Sub(game.startTime);
	durationMs := decimal.NewFromInt(duration.Milliseconds());
	coeff := decimal.NewFromFloat(6E-5);
	e := decimal.NewFromFloat(math.Exp(1));
	multiplier := e.Pow(coeff.Mul(durationMs)).Round(2);
	return multiplier;
}

func (game *Game) getRecentGames(limit int) ([]CrashedGame, error) {
	var games []CrashedGame;

	rows, err := game.db.Query(`
		SELECT id, FLOOR(UNIX_TIMESTAMP(startTime)) as startTime,
		CAST(1000*(endTime - startTime) AS INTEGER) AS duration,
		multiplier, playerCount, winnerCount
		FROM games
		ORDER BY startTime DESC
		LIMIT ?
	`, limit);

	if err != nil {
		game.logger.Log(logging.Entry{
			Payload: Log{
				"msg"  : "Error fetching recent games",
				"error": err,
			},
			Severity: logging.Error,
		});
		return nil, err;
	}

	for rows.Next() {
		var gameRow CrashedGame;
		var startTime int64;
		var multiplier string;
		var duration int64;

		rows.Scan(
			&gameRow.id,
			&startTime,
			&duration,
			&multiplier,
			&gameRow.players,
			&gameRow.winners,
		);

		gameRow.startTime = time.UnixMilli(startTime);
		gameRow.duration = time.Duration(duration * int64(time.Millisecond));
		result, err := decimal.NewFromString(multiplier);

		if err == nil {
			gameRow.multiplier = result;
		} else {
			gameRow.multiplier = decimal.Zero;
		}

		games = append(games, gameRow);
	}

	return games, nil;
}

func (game *Game) saveRecord() (*CrashedGame, error) {
	winners := 0;
	players := len(game.players);

	for i := range(game.players) {
		if game.players[i].cashOut.cashedOut {
			winners++;
		}
	}

	multiplier := game.calculateFinalMultiplier();

	_, err := game.db.Exec(`
		INSERT INTO games
		(id, startTime, endTime, multiplier, playerCount, winnerCount)
		VALUES
		(?, ?, ?, ?, ?, ?)
	`, game.id, game.startTime, game.endTime, multiplier,
		players, winners);

	if err != nil {
		return nil, err;
	}

	record := CrashedGame{
		id: game.id,
		startTime: game.startTime,
		duration: game.endTime.Sub(game.startTime),
		multiplier: multiplier,
		players: players,
		winners: winners,
	};

	return &record, nil;
}

func (game *Game) emitBalanceUpdate(player *Player, newBalance decimal.Decimal) {
	observer, ok := game.observers[player.clientId];

	if ok && observer.socket.Connected() {
		observer.socket.Emit("UpdateBalance", map[string]string{
			"currency": player.currency,
			"balance" : newBalance.String(),
		});
	}
}

func (game *Game) emitBetList() {
	game.Emit("BetList", map[string]any{
		"players": game.players,
		"waiting": game.waiting,
	});
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
