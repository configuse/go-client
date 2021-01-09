package go_client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

const BaseUrl = "https://configuse.tech/api"

type Settings struct {
	url                     string
	ProjectKey              string
	RefreshIntervalTime     time.Duration
	FirstTimeLoadRetryCount int
}

type configuration struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type ConfigurationDto struct {
	key   string
	value string
}

func init() {
	log.SetFormatter(&log.JSONFormatter{})
	log.SetOutput(os.Stdout)
	log.SetLevel(log.TraceLevel)
}

func (c ConfigurationDto) String() string {
	return c.value
}

const IntDefault = 0
const BoolDefault = false

func (c ConfigurationDto) Int() int {
	n, err := strconv.Atoi(c.value)

	if err != nil {
		log.Fatal("Int() ", "an error occurred", "err ", err.Error())
		return IntDefault
	}

	return n
}

func (c ConfigurationDto) Bool() bool {
	n, err := strconv.ParseBool(c.value)

	if err != nil {
		log.Fatal("Bool() ", "an error occurred", "err ", err.Error())
		return BoolDefault
	}

	return n
}

func GetEnvironmentVariable(v string) ConfigurationDto {
	return ConfigurationDto{
		key:   v,
		value: os.Getenv(v),
	}
}

type Environments struct {
	FilePath string
	FileName string
}

var client = &http.Client{
	Timeout: time.Second * 30,
}

func (s Settings) GetRequestUrl() string {
	return s.url + "/configurations/v1/" + s.ProjectKey
}

var configurationDtoList = make(map[string]ConfigurationDto)
var interval = make(chan bool, 1)
var IsInitialized = make(chan bool, 1)
var defaultRequestDelayInSecond = time.Second * 5

func Get(configurationKey string) ConfigurationDto {
	return configurationDtoList[configurationKey]
}

func (s Settings) getConfigurationsFromService() ([]configuration, error) {
	req, err := http.NewRequest("GET", s.GetRequestUrl(), nil)

	if err != nil {
		log.Info("an error occurred while getting configurations. ", "err: ", err.Error())
		return nil, err
	}

	req.Header.Add("x-correlationid", uuid.New().String())
	req.Header.Add("x-agentname", "configUse-go-client")

	startTime := time.Now()
	resp, err := client.Do(req)
	fmt.Println("total time: ", time.Now().Sub(startTime))
	var configurationList []configuration

	if resp != nil {
		err = json.NewDecoder(resp.Body).Decode(&configurationList)

		if err != nil {
			log.Println("an error occurred while getting configurations...", err)
			return []configuration{}, errors.New("an error occurred while decoding response")
		}

		defer resp.Body.Close()

		return configurationList, nil
	}

	return configurationList, errors.New("an error occurred")
}

type loader interface {
	loadConfigurationsFromService(settings Settings)
}

func (s Settings) loadConfigurationsFromService() {
	f := true
	init := false
	counter := 0

	for {
		<-interval
		configurationList, err := s.getConfigurationsFromService()

		if err != nil && f {
			log.Info(fmt.Printf("configurations didn't update counter:%d", counter))

			if counter < s.FirstTimeLoadRetryCount && !init {
				time.Sleep(defaultRequestDelayInSecond)
				counter++
				interval <- true
				continue
			}

			if init {
				time.Sleep(s.RefreshIntervalTime)
				interval <- true
				continue
			}

			log.Fatal("arrived max retry count before first load")
		}

		for _, config := range configurationList {
			if cDto, ok := configurationDtoList[config.Key]; ok {
				if cDto.value != config.Value {
					if !f {
						log.Info(fmt.Printf("changed configuration value. configurationKey: %v -> configuration new & old value: %v --- %v, ", config.Key, config.Value, cDto.value))
					}

					cDto.value = config.Value
					configurationDtoList[config.Key] = cDto
				}
			} else {
				log.Info(fmt.Printf("new configuration found. configurationKey: %v configuration value: %v", config.Key, config.Value))
				configurationDtoList[config.Key] = ConfigurationDto{
					key:   config.Key,
					value: config.Value,
				}
			}
		}

		if f {
			IsInitialized <- true
			init = true
			close(IsInitialized)
			f = false

			if s.RefreshIntervalTime < time.Millisecond*1 {
				s.RefreshIntervalTime = time.Second * 60
				log.Info(fmt.Printf("wrong intervalTimeInSecond value, will be set as: %s", s.RefreshIntervalTime))
			}
		}

		time.Sleep(s.RefreshIntervalTime)
		interval <- true
		continue
	}
}

func Init(s Settings) {
	s.url = BaseUrl
	interval <- true
	go s.loadConfigurationsFromService()
}
