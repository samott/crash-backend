package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"time"

	"log/slog"
	"net/http"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/samott/crash-backend/config"
	"github.com/samott/crash-backend/bank"
	"github.com/samott/crash-backend/game"
	"github.com/samott/crash-backend/rates"

	"database/sql"

	"github.com/golang-jwt/jwt/v5"
	"github.com/spruceid/siwe-go"

	"github.com/go-sql-driver/mysql"
	engineTypes "github.com/zishang520/engine.io/v2/types"
	"github.com/zishang520/socket.io/v2/socket"
	"cloud.google.com/go/logging"
	"github.com/shopspring/decimal"
);

var (
	ErrInvalidParameters = errors.New("invalid parameters")
	ErrInvalidDecimalValue = errors.New("invalid decimal value")
	ErrInvalidCurrency = errors.New("invalid currency")
	ErrInvalidSigningMEthod = errors.New("invalid signing method")
	ErrInvalidJwtToken = errors.New("invalid JWT token")
)

var JWT_SECRET = []byte("1_top_secret");

type Log = map[string]any;

type AuthParams struct {
	message string;
	signature string;
}

type PlaceBetParams struct {
	betAmount decimal.Decimal;
	autoCashOut decimal.Decimal;
	currency string;
}

type WithdrawParams struct {
	amount decimal.Decimal;
	currency string;
}

type LoginParams struct {
	token string;
}

type Session struct {
	wallet string;
}

func validateToken(token string, session *Session) error {
	tokenObj, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidSigningMEthod;
		}

		return JWT_SECRET, nil;
	})

	if err != nil {
		return err;
	}

	claims, ok := tokenObj.Claims.(jwt.MapClaims);

	if !ok || !tokenObj.Valid {
		return ErrInvalidJwtToken;
	}

	wallet := claims["wallet"].(string);

	session.wallet = wallet;

	return nil;
}

func authenticateUser(payload string, signature string) (string, error) {
	message, err := siwe.ParseMessage(payload);

	if err != nil {
		return "", err;
	}

	publicKey, err := message.VerifyEIP191(signature);

	if err != nil {
		return "", err;
	}

	wallet := crypto.PubkeyToAddress(*publicKey).String();

	return wallet, nil;
}

func generateToken(wallet string) (string, error) {
	if len(wallet) == 0 {
		return "", nil;
	}

	tokenObj := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"wallet": wallet,
		"nbf": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour * 24).Unix(),
	});

	token, err := tokenObj.SignedString(JWT_SECRET);

	if err != nil {
		return "", err;
	}

	return token, nil;
}

func validateAuthenticateParams(result *AuthParams, data ...any) (func([]any, error), error) {
	if len(data) == 0 {
		return nil, ErrInvalidParameters;
	}

	params, ok := data[0].(map[string]any);

	if !ok {
		return nil, ErrInvalidParameters;
	}

	message, ok1 := params["message"].(string);
	signature, ok2 := params["signature"].(string);

	if !ok1 || !ok2 {
		return nil, ErrInvalidParameters;
	}

	*result = AuthParams{
		message: message,
		signature: signature,
	};

	callback := extractCallback(1, data...);

	return callback, nil;
}

func validateLoginParams(result *LoginParams, data ...any) (func([]any, error), error) {
	if len(data) == 0 {
		return nil, ErrInvalidParameters;
	}

	params, ok := data[0].(map[string]any);

	if !ok {
		return nil, ErrInvalidParameters;
	}

	token, ok := params["token"].(string);

	if !ok {
		return nil, ErrInvalidParameters;
	}

	*result = LoginParams{
		token: token,
	};

	callback := extractCallback(1, data...);

	return callback, nil;
}

func validatePlaceBetParams(
	result *PlaceBetParams,
	config *config.CrashConfig,
	data ...any,
) (func([]any, error), error) {
	if len(data) == 0 {
		return nil, ErrInvalidParameters;
	}

	params, ok := data[0].(map[string]any);

	if !ok {
		return nil, ErrInvalidParameters;
	}

	betAmountStr, ok1 := params["betAmount"].(string);
	autoCashOutStr, ok2 := params["autoCashOut"].(string);
	currency, ok3 := params["currency"].(string);

	if !ok1 || !ok2 || !ok3 {
		return nil, ErrInvalidParameters;
	}

	betAmount, err1 := decimal.NewFromString(betAmountStr);
	autoCashOut, err2 := decimal.NewFromString(autoCashOutStr);

	if err1 != nil || err2 != nil {
		return nil, ErrInvalidDecimalValue;
	}

	if _, ok := config.Currencies[currency]; !ok {
		return nil, ErrInvalidCurrency;
	}

	*result = PlaceBetParams{
		betAmount: betAmount,
		autoCashOut: autoCashOut,
		currency: currency,
	};

	callback := extractCallback(1, data...);

	return callback, nil;
}

func validateWithdrawParams(
	result *WithdrawParams,
	config *config.CrashConfig,
	data ...any,
) (func([]any, error), error) {
	if len(data) == 0 {
		return nil, ErrInvalidParameters;
	}

	params, ok := data[0].(map[string]any);

	if !ok {
		return nil, ErrInvalidParameters;
	}

	amountStr, ok1 := params["betAmount"].(string);
	currency, ok2 := params["currency"].(string);

	if !ok1 || !ok2 {
		return nil, ErrInvalidParameters;
	}

	amount, err := decimal.NewFromString(amountStr);

	if err != nil {
		return nil, ErrInvalidDecimalValue;
	}

	if _, ok := config.Currencies[currency]; !ok {
		return nil, ErrInvalidCurrency;
	}

	*result = WithdrawParams{
		amount: amount,
		currency: currency,
	};

	callback := extractCallback(1, data...);

	return callback, nil;
}

func extractCallback(index int, data ...any) func([]any, error) {
	if len(data) != index + 1 {
		return nil;
	}

	callback, ok := data[index].(func([]any, error));

	if !ok {
		// Ought to be an error, but we'll treat it
		// as if there were no callback supplied
		return nil;
	}

	return callback;
}

func corsWrapper(handler http.HandlerFunc, cfg *config.CrashConfig) http.HandlerFunc {
	return func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Access-Control-Allow-Origin", cfg.Cors.Origin);
		handler(res, req);
	};
}

func main() {
	slog.Info("Crash running...");

	configFile := flag.String("configfile", "crash.yaml", "path to configuration file");

	flag.Parse();

	config, err := config.LoadConfig(*configFile);

	if err != nil {
		slog.Error("Failed to load config file " + *configFile);
		return;
	}

	gcpClient, err := logging.NewClient(
		context.Background(),
		config.Logging.ProjectId,
	);

	if err != nil {
		slog.Error("Failed to create logging client", "error", err);
		return;
	}

	var logger *logging.Logger;

	if config.Logging.LocalOnly {
		logger = gcpClient.Logger(
			config.Logging.LogId,
			logging.RedirectAsJSON(os.Stdout),
		);
	} else {
		logger = gcpClient.Logger(config.Logging.LogId);
	}

	defer gcpClient.Close();

	dbConfig := mysql.Config{
		User: config.Database.User,
		DBName: config.Database.DBName,
		Addr: config.Database.Addr,
		AllowNativePasswords: true,
	};

	db, err := sql.Open("mysql", dbConfig.FormatDSN());

	if err != nil {
		slog.Error("Failed to connect to database", "error", err)
		return;
	}

	defer db.Close();

	ratesSvc := rates.NewService((*rates.RatesConfig)(&config.Rates));
	newRates, err := ratesSvc.FetchRates();

	if err != nil {
		slog.Error("Failed to fetch rates", "error", err);
		return;
	}

	err = ratesSvc.SaveRates(newRates, db);

	if err != nil {
		slog.Error("Failed to save rates", "error", err);
		return;
	}

	var ratesTicker time.Ticker;

	if (config.Timers.RatesCheckFrequencyMins > 0) {
		ratesTicker := time.NewTicker(time.Duration(config.Timers.RatesCheckFrequencyMins) * time.Minute);

		go func() {
			for range ratesTicker.C {
				logger.Log(logging.Entry{
					Payload: Log{
						"msg": "Fetching currency rates...",
					},
					Severity: logging.Info,
				});

				newRates, err := ratesSvc.FetchRates();

				if err != nil {
					ratesSvc.SaveRates(newRates, db);
				}
			}
		}();
	}

	defer func() {
		if (config.Timers.RatesCheckFrequencyMins > 0) {
			ratesTicker.Stop();
		}
	}();

	options := socket.DefaultServerOptions();
	options.SetAllowEIO3(true)
	options.SetCors(&engineTypes.Cors{
		Origin:      config.Cors.Origin,
		Credentials: true,
	});

	io := socket.NewServer(nil, options);
	bankObj, err := bank.NewBank(db);

	if err != nil {
		slog.Error("Failed to init bank");
		return;
	}

	gameObj, err := game.NewGame(io, db, config, logger, game.Bank(bankObj));

	if err != nil {
		slog.Error("Failed to init game");
		return;
	}

	http.HandleFunc("/nonce", corsWrapper(nonceHttpHandler, config));

	http.Handle("/socket.io/", io.ServeHandler(nil));
	go http.ListenAndServe(":4000", nil);

	io.On("connection", func(clients ...any) {
		client := clients[0].(*socket.Socket);

		logger.Log(logging.Entry{
			Payload: Log{
				"msg"     : "Client connected",
				"clientId": client.Id(),
			},
			Severity: logging.Info,
		});

		gameObj.HandleConnect(client);

		client.On("authenticate", func(data ...any) {
			authenticateHandler(client, logger, gameObj, data...);
		});

		client.On("disconnected", func(...any) {
			logger.Log(logging.Entry{
				Payload: Log{
					"msg"     : "Client disconnected",
					"clientId": client.Id(),
				},
				Severity: logging.Info,
			});
			gameObj.HandleDisconnect(client);
		});

		var session Session;

		client.On("login", func(data ...any) {
			slog.Info("Client logging in", "client", client.Id);

			var params LoginParams;
			callback, err := validateLoginParams(&params, data...);

			if err := validateToken(params.token, &session); err != nil {
				slog.Warn("Invalid session");

				if callback != nil {
					callback(
						[]any{ map[string]any{
							"success": false,
						} },
						nil,
					);
				}

				return;
			}

			gameObj.HandleLogin(client, session.wallet);

			logger.Log(logging.Entry{
				Payload: Log{
					"msg"   : "User logged in",
					"wallet": session.wallet,
				},
				Severity: logging.Info,
			});

			client.On("refreshToken", func(data ...any) {
				refreshTokenHandler(client, session, logger, gameObj, data...);
			});

			client.On("placeBet", func(data ...any) {
				placeBetHandler(client, session, logger, gameObj, data...);
			});

			client.On("cancelBet", func(data ...any) {
				cancelBetHandler(client, session, logger, gameObj, data...);
			});

			client.On("cashOut", func(data ...any) {
				cashOutHandler(client, session, logger, gameObj, data...);
			});

			client.On("withdraw", func(data ...any) {
				withdrawHandler(client, session, logger, bankObj, config, db, data...);
			});

			if callback != nil {
				callback(
					[]any{ map[string]any{
						"success": err == nil,
					} },
					nil,
				);
			}
		});
	});

	exit := make(chan struct{});
	<- exit;
}
