package main;

import (
	"net/http"
	"encoding/json"

	"github.com/samott/crash-backend/game"
	"github.com/samott/crash-backend/bank"
	"github.com/samott/crash-backend/config"
	"github.com/zishang520/socket.io/v2/socket"
	"cloud.google.com/go/logging"

	"github.com/spruceid/siwe-go"
);

func nonceHttpHandler(w http.ResponseWriter, r *http.Request) {
	var nonce = siwe.GenerateNonce();
	var result, err = json.Marshal(map[string]string{
		"nonce": nonce,
	});

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return;
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(result);
}

func authenticateHandler(
	client *socket.Socket,
	logger *logging.Logger,
	_ *game.Game,
	data ...any,
) {
	logger.Log(logging.Entry{
		Payload: Log{
			"msg"   : "Client authenticating...",
			"client": client.Id(),
		},
		Severity: logging.Info,
	});

	var params AuthParams;

	callback, err := validateAuthenticateParams(&params, data...);

	if err != nil {
		logger.Log(logging.Entry{
			Payload: Log{
				"msg"   : "Invalid parameters",
				"client": client.Id(),
			},
			Severity: logging.Warning,
		});
		client.Disconnect(true);
		return;
	}

	wallet, err := authenticateUser(params.message, params.signature);

	if err != nil {
		logger.Log(logging.Entry{
			Payload: Log{
				"msg"   : "Invalid signature",
				"client": client.Id(),
			},
			Severity: logging.Warning,
		});

		client.Emit("authenticate", map[string]any{
			"success": false,
		});
		return;
	}

	token, err := generateToken(wallet);

	if err != nil {
		logger.Log(logging.Entry{
			Payload: Log{
				"msg"   : "Error generating token",
				"client": client.Id(),
				"error" : err,
			},
			Severity: logging.Error,
		});

		client.Emit("authenticate", map[string]any{
			"success": false,
		});
		return;
	}

	logger.Log(logging.Entry{
		Payload: Log{
			"msg"   : "Authentication successful",
			"client": client.Id(),
		},
		Severity: logging.Info,
	});

	if callback != nil {
		callback(
			[]any{ map[string]any{
				"token": token,
				"success": true,
			} },
			nil,
		);
	}
}

func disconnectedHandler(
	client *socket.Socket,
	logger *logging.Logger,
	gameObj *game.Game,
	_ ...any,
) {
	logger.Log(logging.Entry{
		Payload: Log{
			"msg"   : "Client disconnected",
			"client": client.Id(),
		},
		Severity: logging.Info,
	});

	gameObj.HandleDisconnect(client);
};

func refreshTokenHandler(
	client *socket.Socket,
	session Session,
	logger *logging.Logger,
	_ *game.Game,
	data ...any,
) {
	logger.Log(logging.Entry{
		Payload: Log{
			"msg"   : "Refreshing JWT token",
			"client": client.Id(),
			"wallet": session.wallet,
		},
		Severity: logging.Info,
	});

	token, err := generateToken(session.wallet);

	if err != nil {
		logger.Log(logging.Entry{
			Payload: Log{
				"msg"   : "Error generating token",
				"client": client.Id(),
				"error" : err,
			},
			Severity: logging.Error,
		});

		client.Emit("authenticate", map[string]any{
			"success": false,
		});

		return;
	}

	callback := extractCallback(0, data...);

	if callback != nil {
		callback(
			[]any{ map[string]any{
				"token": token,
				"success": true,
			} },
			nil,
		);
	}
}

func placeBetHandler(
	client *socket.Socket,
	session Session,
	logger *logging.Logger,
	gameObj *game.Game,
	data ...any,
) {
	logger.Log(logging.Entry{
		Payload: Log{
			"msg"   : "PlaceBet for user",
			"client": client.Id(),
			"params": data,
		},
		Severity: logging.Info,
	});

	var params PlaceBetParams;

	callback, err := validatePlaceBetParams(&params, gameObj.GetConfig(), data...);

	if err != nil {
		logger.Log(logging.Entry{
			Payload: Log{
				"msg"   : "Invalid parameters",
				"client": client.Id(),
			},
			Severity: logging.Warning,
		});

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
}

func cancelBetHandler(
	client *socket.Socket,
	session Session,
	logger *logging.Logger,
	gameObj *game.Game,
	data ...any,
) {
	logger.Log(logging.Entry{
		Payload: Log{
			"msg"   : "CancelBet for user",
			"client": client.Id(),
			"wallet": session.wallet,
			"params": data,
		},
		Severity: logging.Info,
	});

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
}

func cashOutHandler(
	client *socket.Socket,
	session Session,
	logger *logging.Logger,
	gameObj *game.Game,
	data ...any,
) {
	logger.Log(logging.Entry{
		Payload: Log{
			"msg"   : "CashOut for user",
			"client": client.Id(),
			"wallet": session.wallet,
			"params": data,
		},
		Severity: logging.Info,
	});

	gameObj.HandleCashOut(session.wallet);
}

func withdrawHandler(
	client *socket.Socket,
	session Session,
	logger *logging.Logger,
	bankObj *bank.Bank,
	cfg *config.CrashConfig,
	data ...any,
) {
	logger.Log(logging.Entry{
		Payload: Log{
			"msg"   : "Withdraw for user",
			"client": client.Id(),
			"params": data,
		},
		Severity: logging.Info,
	});

	var params WithdrawParams;

	callback, err := validateWithdrawParams(&params, cfg, data...);

	if err != nil {
		logger.Log(logging.Entry{
			Payload: Log{
				"msg"   : "Invalid parameters",
				"client": client.Id(),
			},
			Severity: logging.Warning,
		});

		client.Disconnect(true);
		return;
	}

	balance, err := bankObj.GetBalance(
		session.wallet,
		params.currency,
	);

	if err != nil {
		if callback != nil {
			callback(
				[]any{ map[string]any{
					"success": false,
					"errorCode": "INTERNAL_ERROR",
				} },
				nil,
			);
		}
		return;
	}

	if balance.LessThan(params.amount) {
		if callback != nil {
			callback(
				[]any{ map[string]any{
					"success": false,
					"errorCode": "INSUFFICIENT_BALANCE",
				} },
				nil,
			);
		}
		return;
	}

	_, err = bankObj.WithdrawBalance(
		session.wallet,
		params.currency,
		params.amount,
	);

	if err != nil {
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

	if callback != nil {
		callback(
			[]any{ map[string]any{
				"success": err == nil,
			} },
			nil,
		);
	}
}
