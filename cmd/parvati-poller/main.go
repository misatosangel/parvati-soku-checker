// Copyright 2017-2020 misatos.angel@gmail.com.  All rights reserved.
//
//
// This small program sits on loop querying hosts from Parvati's API
// testing their status and then updating the new status.
//
package main

import (
	"fmt"
	"github.com/jessevdk/go-flags"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime/pprof"
	"syscall"
	"time"

	"github.com/misatosangel/parvati-api-client/pkg/parvatigo"
	"github.com/misatosangel/parvati-api-client/pkg/swagger"
	"github.com/misatosangel/soku-net-checker/pkg/checker"
)

// Variables used for command line parameters
var settings struct {
	Username   string        `short:"u" long:"username" required:"false" description:"Parvati username" value-name:"<nick>"`
	URI        string        `long:"uri"  required:"false" description:"Parvati API Uri" value-name:"<url>"`
	ConfigFile string        `short:"c" long:"config" required:"false" value-name:"<path>" description:"Location of a gitconfig style file holding your credentials and password and other preferences."`
	OneShot    bool          `short:"o" long:"once" description:"Just check once, do not keep checking"`
	Frequency  time.Duration `short:"f" default:"5s" value-name:"<duration>" long:"frequency" description:"How often to keep checking"`
	Version    func()        `long:"version" required:"false" description:"Print tool version and exit."`
	Timeout    time.Duration `long:"timeout" default:"1s" value-name:"<duration>" description:"How long to wait for responses, default 1s."`
	Debug      bool          `short:"d" long:"debug" description:"Lots of verbose info, implies --api-debug."`
	Updates    bool          `long:"update" description:"Actually commit back updates."`
	APIDebug   bool          `long:"api-debug" description:"Debug API load errors."`
	Threads    uint8         `short:"t" long:"threads" default:"5" description:"Number of threads to use."`
}

type Job struct {
	Roll         string
	Request      *checker.Request
	ToPoint      uint
	OrigHostStat swagger.HosterStatus
	WaitStat     swagger.WaiterStatus
	Game         *swagger.Game
}

var buildVersion = "dev"
var buildDate = "dev"
var buildCommit = "dev"

func init() {
}

func main() {
	os.Exit(run())
}

func run() int {
	// this will fatal or exit on non-zero or help
	CliParse()
	api, config, err := LoadParvatiApi()
	if err != nil {
		log.Fatalln("Failed to init parvati API:\n" + err.Error())
	}
	if settings.Debug {
		settings.APIDebug = true
	}
	if settings.APIDebug {
		api.Verbose = true
	}
	soku := FindSokuGameOrDie(api)
	jobQueue := make(chan *Job, settings.Threads+1)
	var i uint8
	for i = 0; i < settings.Threads; i++ {
		go Worker(api, i, jobQueue)
	}

	signalC := make(chan os.Signal, 1)
	signal.Notify(signalC, os.Interrupt)
	signalUSR1 := make(chan os.Signal, 1)
	signal.Notify(signalUSR1, syscall.SIGUSR1)
	checkTicket := time.NewTicker(settings.Frequency)
	defer close(jobQueue)
	log.Printf("Connecting (announce: %s) to %s\n", config.Announcer, api.Info())
	if !settings.OneShot {
		log.Printf("Starting continuous checker, pid: %d, use CTRL+C to stop or end USR1 for thread-dump.\n", os.Getpid())
	}
	if !settings.Updates {
		log.Printf("! Running in read-only mode, will not update.\n")
	}
	for {
		select {
		case <-checkTicket.C:
			if settings.Debug {
				log.Printf("Grabbing current hostlist\n")
			}
			list, err := api.CheckListedHosts(soku, nil)
			if err != nil {
				if settings.OneShot {
					log.Fatalln("Failed to get game list:\n" + err.Error())
				}
				log.Printf("Failed to get game list:\n" + err.Error())
				continue
			}
			if settings.Debug {
				cnt := len(list.Hosts)
				log.Printf("Found %d active hoster(s)\n", cnt)
			}
			for _, hosterStatus := range list.Hosts {
				req, err := HostToCheckReq(&hosterStatus.Host)
				if err != nil {
					log.Printf("Could not parse IP from host: %s", err.Error())
					continue
				}
				job := &Job{
					Roll:         hosterStatus.Host.Version,
					Request:      req,
					ToPoint:      checker.STATE_SPEC_REACH_RELAY,
					OrigHostStat: hosterStatus,
					Game:         soku,
				}
				jobQueue <- job
			}
			cnt := len(list.Waits)
			if settings.Debug {
				log.Printf("Found %d active waiter(s)\n", cnt)
			}
			if cnt > 0 {
				now := time.Now().UTC()
				for _, waiterStatus := range list.Waits {
					wExpire := waiterStatus.Waiter.WaitUntil.UTC()
					if wExpire.Before(now) {
						job := &Job{
							WaitStat: waiterStatus,
							Game:     soku,
						}
						jobQueue <- job
					}
				}
			}
			if settings.OneShot {
				return 0
			}
		case sig := <-signalUSR1:
			log.Printf("=== received " + sig.String() + " ===\n*** blocking goroutine dump ***\n")
			pprof.Lookup("block").WriteTo(os.Stderr, 1)
			log.Printf("*** full goroutine dump ***\n")
			pprof.Lookup("goroutine").WriteTo(os.Stderr, 1)
			log.Printf("*** end full goroutine dump ***\n")
		case sig := <-signalC:
			fmt.Println("Stopping on signal:", sig)
			return 0
		}
	}

	return 0

}

// Create a check request form the given host information
// Match up roll version
func HostToCheckReq(host *swagger.Host) (*checker.Request, error) {
	hostAddr := host.Ipv4 // we know soku can only IPv4 but just in case
	if hostAddr == "" {
		hostAddr := host.Ipv6
		if hostAddr == "" {
			return nil, fmt.Errorf("Host id %d (name: %s) has no IPv4 or v6 address", host.BaseInfo.Id, host.BaseInfo.DisplayName)
		}
	}
	if settings.Debug {
		log.Printf("Host id %d (name: %s) has Address: %s", host.BaseInfo.Id, host.BaseInfo.DisplayName, hostAddr)
	}
	addr := net.JoinHostPort(hostAddr, fmt.Sprintf("%d", host.Port))
	req, err := checker.NewRequest(addr)
	if err != nil {
		return nil, err
	}
	req.Timeout = settings.Timeout
	return req, nil
}

func FindSokuGameOrDie(api *parvatigo.Api) *swagger.Game {
	games, err := api.GetGames()
	if err != nil {
		log.Fatalln("Failed to get game list:\n" + err.Error())
	}
	for _, game := range games {
		if game.UrlShortName == "soku" {
			return &game
		}
	}
	failMsg := "Failed to find soku in supported games list, found:\n"
	for _, game := range games {
		failMsg += game.UrlShortName + "\n"
	}
	log.Fatalln("Failed to get game list:\n" + err.Error())
	return nil // can't get here anyway
}

// Main worker loops
// Spawn worker thread, listen on the queue until a null job comes through or the queue closes
func Worker(api *parvatigo.Api, tid uint8, queue chan *Job) {

	for j := range queue {
		if j == nil {
			return
		}
		if j.Request == nil {
			// a waiter
			if !settings.Updates {
				log.Printf("Thread: %d [NOT UPDATING] terminating wait by: '%s' (%d)", tid, j.WaitStat.Waiter.DisplayName, j.WaitStat.Waiter.User.Id)
				continue
			}
			if settings.Debug {
				log.Printf("Thread: %d terminating wait by: '%s' (%d)", tid, j.WaitStat.Waiter.DisplayName, j.WaitStat.Waiter.User.Id)
			}
			apiErr := api.UpdateWaitTime(j.Game, j.WaitStat.Waiter.User.Id, 0, "")
			if apiErr != nil {
				log.Printf("Thread: %d terminating wait by: '%s' (%d) failed: %s", tid, j.WaitStat.Waiter.DisplayName, j.WaitStat.Waiter.User.Id, apiErr.Error())
			}
			continue
		}
		su, err := CheckHost(j)
		if err != nil {
			log.Printf("Thread: %d checking on host: '%s' failed: %s", tid, j.Request.Address, err.Error())
			if su != nil {
			}
			continue
		}
		if settings.Debug {
			log.Printf("Thread: %d check on host '%s' resulted in status: %s (was: %s)", tid, j.Request.Address, su.Status, j.OrigHostStat.Status.Status)
		}
		if !settings.Updates {
			spec := "unknown"
			if su.CanSpec != nil {
				if *su.CanSpec {
					spec = "yes"
				} else {
					spec = "no"
				}
			}
			vers := "unknown"
			if su.NewVers != nil {
				vers = *su.NewVers
			}
			p1name := "(none)"
			p2name := "(none)"
			if su.Prof1Name != nil {
				p1name = *su.Prof1Name
			}
			if su.Prof2Name != nil {
				p2name = *su.Prof2Name
			}

			log.Printf("Thread: %d [NOT UPDATING] check on host '%s' resulted in status: %s (was: %s). Opponent Addr: %s, Spec: %s. Vers: %s. Prof1: %s Prof2: %s", tid, j.Request.Address, su.Status, j.OrigHostStat.Status.Status, su.OpponentAddr, spec, vers, p1name, p2name)
			continue
		}
		ret, apiErr := api.UpdateHostStatus(j.Game, *su)
		if apiErr != nil {
			log.Printf("Thread: %d update host status for '%s' failed: %s", tid, j.Request.Address, apiErr.Error())
			continue
		}
		if settings.Debug {
			log.Printf("Thread: %d check on host '%s' resulted in check id %d status: %s (was: %s). Spec: %s. Vers: %s. Prof1: %s Prof2: %s\n", tid, j.Request.Address, ret.Id, ret.Status, j.OrigHostStat.Status.Status, ret.CanSpec, ret.Version, ret.P1Profile, ret.P2Profile)
		}
	}
}

// Attempts to check the host and turn it into a parvati host update structure
func CheckHost(j *Job) (*parvatigo.StatusUpdate, error) {
	request, err := checker.NewRequest(j.Request.Address)
	if err != nil {
		return nil, err
	}
	var result checker.CheckResult
	if j.Roll == "" || j.Roll == "unknown" {
		result = request.Check(j.ToPoint, false)
	} else {
		result = request.CheckVersion(j.ToPoint, j.Roll, false)
	}
	if settings.Debug {
		log.Printf("Result: %s", result.String())
	}
	su := &parvatigo.StatusUpdate{
		CheckDate:   time.Now(),
		Status:      result.Status,
		HosterId:    j.OrigHostStat.Host.BaseInfo.Id,
		LastCheckId: j.OrigHostStat.Status.Id,
	}
	if result.Version != "" {
		su.NewVers = &result.Version
	}
	if result.GoodStatus() {
		switch result.Spectate {
		case 'y':
			s := true
			su.CanSpec = &s
		case 'n':
			s := false
			su.CanSpec = &s
		}
	}
	if len(result.Profiles) > 0 {
		p1 := result.Profiles[0]
		p2 := result.Profiles[1]
		if p1 != "" {
			su.Prof1Name = &p1
		}
		if p2 != "" {
			su.Prof2Name = &p2
		}
	}
	if result.Opponent != "" {
		su.OpponentAddr = result.Opponent
	}
	return su, nil
}

func LoadParvatiApi() (*parvatigo.Api, *parvatigo.ApiConfig, error) {
	var config *parvatigo.ApiConfig
	if settings.ConfigFile == "" {
		var err error
		config, err = parvatigo.ReadDefaultConfig()
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, nil, err
			}
			// no config and no parvati credentials, so just do default list show and leave
			def, _ := parvatigo.DefaultConfigFile()
			return nil, nil, fmt.Errorf("No default config file (expected: %s), and no config file given with --config / -c\n"+
				"You must supply a standard gitconfig style file containing at least a value for parvatigo.password\n", def)
		}
	} else {
		var err error
		config, err = parvatigo.ReadConfig(settings.ConfigFile)
		if err != nil {
			return nil, nil, err
		}
	}
	if config != nil && config.Password == "" {
		return nil, config, fmt.Errorf("Configuration file did not specify a password with key parvatigo.password\n")
	}
	if settings.URI != "" {
		config.URI = settings.URI
	}
	if settings.Username != "" {
		config.Username = settings.Username
	}
	api, err := parvatigo.NewApi(config, buildVersion)
	if err != nil {
		return nil, config, err
	}
	return &api, config, err
}

func CliParse() {
	parser := flags.NewParser(&settings, flags.Default)
	gaveVersion := false
	settings.Version = func() {
		parser.SubcommandsOptional = true
		fmt.Printf("Parvati soku checker version %s\nBuilt: %s\nCommit: %s\n", buildVersion, buildDate, buildCommit)
		gaveVersion = true
	}
	_, err := parser.Parse()
	if err != nil {
		switch err.(type) {
		case *flags.Error:
			if err.(*flags.Error).Type == flags.ErrHelp {
				os.Exit(0)
			}
		}
		log.Fatalln(err)
	}
}
