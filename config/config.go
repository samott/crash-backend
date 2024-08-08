package config;

import (
	"os"
	"gopkg.in/yaml.v3"
);

type CurrencyDef struct {
	Name string `yaml:"name"`;
	Units string `yaml:"units"`;
	CoinId uint32 `yaml:"coinId"`;
	Decimals uint `yaml:"decimals"`;
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

	Currencies map[string]CurrencyDef `yaml:"currencies"`;

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

	OnChain struct {
		RpcUrl string `yaml:"rpcUrl"`;
		ChainId int64 `yaml:"chainId"`;
		Contract string `yaml:"contract"`;
	} `yaml:"onChain"`

	Timers struct {
		RatesCheckFrequencyMins int `yaml:"ratesCheckFrequencyMins"`;
	}
};

func LoadConfig(configFile string) (*CrashConfig, error) {
	data, err := os.ReadFile(configFile);

	var config CrashConfig;

	if err != nil {
		return nil, err;
	}

	yaml.Unmarshal(data, &config);

	return &config, nil;
}
