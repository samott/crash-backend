package main;

import (
	"os"
	"time"
	"errors"
	"strings"
	"context"
	"flag"

	"log/slog"
	"net/http"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/samott/crash-backend/rates"
	"github.com/samott/crash-backend/game"
	"github.com/samott/crash-backend/bank"

	"database/sql"

	"github.com/spruceid/siwe-go"
	"github.com/golang-jwt/jwt/v5"

	"github.com/go-sql-driver/mysql"
	"github.com/zishang520/socket.io/v2/socket"
	engineTypes "github.com/zishang520/engine.io/v2/types"

	"cloud.google.com/go/logging"

	"gopkg.in/yaml.v3"

	"github.com/shopspring/decimal"
);

var JWT_SECRET = []byte("1_top_secret");

var currencies map[string]bool;

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

type Session struct {
	wallet string;
}

type CrashConfig struct {
	Database struct {
		User string `yaml:"username"`;
		DBName string `yaml:"database"`;
		Addr string `yaml:"password"`;
	}

	Cors struct {
		Origin string `yaml:"origin"`;
	}

	Rates struct {
		ApiKey string `yaml:"apiKey"`;
		Cryptos map[string]string `yaml:"cryptos"`;
		Fiats []string `yaml:"fiats"`;
	}

	Logging struct {
		LocalOnly bool `yaml:"localOnly"`;
		ProjectId string `yaml:"projectId"`;
		LogId string `yaml:"logId"`;
	}
};

func validateToken(token string, session *Session) error {
	tokenObj, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("Incorrect signing method");
		}

		return JWT_SECRET, nil;
	})

	if err != nil {
		return err;
	}

	claims, ok := tokenObj.Claims.(jwt.MapClaims);

	if !ok || !tokenObj.Valid {
		return errors.New("Invalid JWT token");
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

	bytes := crypto.FromECDSAPub(publicKey);
	wallet := hexutil.Encode(bytes);

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
		return nil, errors.New("Invalid parameters");
	}

	params, ok := data[0].(map[string]any);

	if !ok {
		return nil, errors.New("Invalid parameters");
	}

	message, ok1 := params["message"].(string);
	signature, ok2 := params["signature"].(string);

	if !ok1 || !ok2 {
		return nil, errors.New("Invalid parameters");
	}

	*result = AuthParams{
		message: message,
		signature: signature,
	};

	callback := extractCallback(1, data...);

	return callback, nil;
}

func validatePlaceBetParams(result *PlaceBetParams, data ...any) (func([]any, error), error) {
	if len(data) == 0 {
		return nil, errors.New("Invalid parameters");
	}

	params, ok := data[0].(map[string]any);

	if !ok {
		return nil, errors.New("Invalid parameters");
	}

	betAmountStr, ok1 := params["betAmount"].(string);
	autoCashOutStr, ok2 := params["autoCashOut"].(string);
	currency, ok3 := params["currency"].(string);

	if !ok1 || !ok2 || !ok3 {
		return nil, errors.New("Invalid parameters");
	}

	betAmount, err1 := decimal.NewFromString(betAmountStr);
	autoCashOut, err2 := decimal.NewFromString(autoCashOutStr);

	if err1 != nil || err2 != nil {
		return nil, errors.New("Invalid decimal numbers");
	}

	if _, ok := currencies[currency]; !ok {
		return nil, errors.New("Unsupported currency");
	}

	*result = PlaceBetParams{
		betAmount: betAmount,
		autoCashOut: autoCashOut,
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

func loadConfig(configFile string) (*CrashConfig, error) {
	data, err := os.ReadFile(configFile);

	var config CrashConfig;

	if err != nil {
		return nil, err;
	}

	yaml.Unmarshal(data, &config);

	return &config, nil;
}

func main() {
	slog.Info("Crash running...");

	currencies = map[string]bool{
		"eth": true,
	};

	configFile := flag.String("configfile", "crash.yaml", "path to configuration file");

	flag.Parse();

	config, err := loadConfig(*configFile);

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
	_, err = ratesSvc.FetchRates();

	if err != nil {
		slog.Error("Failed to fetch rates", "error", err);
		return;
	}

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

	gameObj, err := game.NewGame(io, db, game.Bank(bankObj));

	if err != nil {
		slog.Error("Failed to init game");
		return;
	}

	http.HandleFunc("/nonce", nonceHttpHandler);

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

		headers := client.Handshake().Headers;

		header, headerFound := headers["Authorization"];

		if !headerFound {
			gameObj.HandleConnect(client, "");

			client.On("authenticate", func(data ...any) {
				authenticateHandler(client, *gameObj, data...);
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

			return;
		}

 		if len(header) == 0 || !strings.HasPrefix(header[0], "Bearer ") {
			slog.Warn("Missing auth header");
			client.Disconnect(true);
			return;
		}

		token := strings.TrimPrefix(header[0], "Bearer ");

		var session Session;

		if err := validateToken(token, &session); err != nil {
			slog.Warn("Invalid session");
			client.Disconnect(true);
			return;
		}

		logger.Log(logging.Entry{
			Payload: Log{
				"msg"   : "User authenticated",
				"wallet": session.wallet,
			},
			Severity: logging.Info,
		});

		gameObj.HandleConnect(client, session.wallet);

		client.On("refreshToken", func(data ...any) {
			refreshTokenHandler(client, session, *gameObj, data...);
		});

		client.On("placeBet", func(data ...any) {
			placeBetHandler(client, session, *gameObj, data...);
		});

		client.On("cancelBet", func(data ...any) {
			cancelBetHandler(client, session, *gameObj, data...);
		});

		client.On("cashOut", func(data ...any) {
			cashOutHandler(client, session, *gameObj, data...);
		});
	});

	exit := make(chan struct{});
	<- exit;
}
