package main;

import (
	"time"
	"errors"
	"strings"

	"log/slog"
	"net/http"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/samott/crash-backend/game"
	"github.com/samott/crash-backend/bank"

	"database/sql"

	"github.com/spruceid/siwe-go"
	"github.com/golang-jwt/jwt/v5"

	"github.com/go-sql-driver/mysql"
	"github.com/zishang520/socket.io/v2/socket"

	"github.com/shopspring/decimal"
);

var JWT_SECRET = []byte("1_top_secret");

var currencies map[string]bool;

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

func main() {
	slog.Info("Crash running...");

	currencies = map[string]bool{
		"eth": true,
	};

	dbConfig := mysql.Config{
		User: "crash",
		DBName: "crash",
		Addr: "localhost",
		AllowNativePasswords: true,
	};

	var db, err = sql.Open("mysql", dbConfig.FormatDSN());

	if err != nil {
		slog.Error("Failed to connect to database", "error", err)
		return;
	}

	defer db.Close();

	io := socket.NewServer(nil, nil);
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

	http.Handle("/socket.io/", io.ServeHandler(nil));
	go http.ListenAndServe(":4000", nil);

	io.On("connection", func(clients ...any) {
		client := clients[0].(*socket.Socket);

		slog.Info("Client connected", "clientId", client.Id);

		headers := client.Handshake().Headers;

		header, headerFound := headers["Authorization"];

		if !headerFound {
			gameObj.HandleConnect(client, "");

			client.On("authenticate", func(data ...any) {
				slog.Info("Client authenticating", "client", client.Id);

				var params AuthParams;

				callback, err := validateAuthenticateParams(&params, data...);

				if err != nil {
					slog.Warn("Invalid parameters", "client", client.Id);
					client.Disconnect(true);
					return;
				}

				wallet, err := authenticateUser(params.message, params.signature);

				if err != nil {
					slog.Warn("Invalid signature", "client", client.Id);
					client.Emit("authenticate", map[string]any{
						"success": false,
					});
					return;
				}

				token, err := generateToken(wallet);

				if err != nil {
					slog.Error("Error generating token", "err", err);
					client.Emit("authenticate", map[string]any{
						"success": false,
					});
					return;
				}

				slog.Info("Authentication successful", "client", client.Id);

				if callback != nil {
					callback(
						[]any{ map[string]any{
							"token": token,
							"success": true,
						} },
						nil,
					);
				}
			});

			client.On("disconnected", func(...any) {
				slog.Info("Client disconnected", "client", client);
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

		slog.Info("User authenticated", "wallet", session.wallet);

		gameObj.HandleConnect(client, session.wallet);

		client.On("placeBet", func(data ...any) {
			slog.Info("PlaceBet for user", "wallet", session.wallet);

			var params PlaceBetParams;

			callback, err := validatePlaceBetParams(&params, data...);

			if err != nil {
				slog.Warn("Invalid parameters", "client", client.Id);
				client.Disconnect(true);
				return;
			}

			err = gameObj.HandlePlaceBet(
				client,
				session.wallet,
				params.currency,
				params.betAmount,
				params.autoCashOut,
			);

			if callback != nil {
				callback(
					[]any{ map[string]any{
						"success": err == nil,
					} },
					nil,
				);
			}
		});

		client.On("cancelBet", func(data ...any) {
			slog.Info("CancelBet for user", "wallet", session.wallet);

			err := gameObj.HandleCancelBet(session.wallet);

			callback := extractCallback(0, data...);

			if callback != nil {
				callback(
					[]any{ map[string]any{
						"success": err == nil,
					} },
					nil,
				);
			}
		});

		client.On("cashOut", func(...any) {
			slog.Info("CashOut for user", "wallet", session.wallet);
			gameObj.HandleCashOut(session.wallet);
		});

		client.On("disconnected", func(...any) {
			slog.Info("Client disconnected", "client", client);
			gameObj.HandleDisconnect(client);
		});
	});

	exit := make(chan struct{});
	<- exit;
}
