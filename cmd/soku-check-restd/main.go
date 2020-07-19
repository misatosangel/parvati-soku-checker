// Copyright 2017-2020 misatos.angel@gmail.com.  All rights reserved.
//
// Simple httpd rest api over being able to check the state of a soku
// host. This basic implementation exposes:
// - /ping/<address> (anyone can call)
// - /check/<address> (anyone can call but parvati api creds can be provided)
//
// The checker will not return opponent IPs unless the checking user has a
// valid credential which enables see_user_private_hosts.
//
package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	"github.com/jessevdk/go-flags"

	"github.com/misatosangel/parvati-soku-checker/pkg/pretty"
	"github.com/misatosangel/soku-cardinfo/pkg/card-info"
	"github.com/misatosangel/soku-net-checker/pkg/checker"
)

// Variables used for command line parameters
var settings struct {
	BindAddr  string `short:"b" long:"bind" description:"Address to bind to"`
	AuthCheck string `short:"a" long:"auth-url" description:"Auth to check credentials against"`
	Live      bool   `short:"r" long:"release" description:"Run in release mode"`
	CardInfo  string `long:"cards" required:"true" description:"Location of a CSV cards file to read."`
}

func init() {
}

func main() {
	os.Exit(run())
}

func run() int {
	CliParse()

	csvFile, err := os.Open(settings.CardInfo)
	if err != nil {
		log.Fatal("Unable to open card data CSV file:", err)
	}
	allCards, err := cardinfo.NewFromCSV(csvFile)
	if err != nil {
		log.Fatal("Unable to read card data CSV file:", err)
	}

	if settings.Live {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()
	//mainLogger := log.New( os.Stderr, "Httpd: ", log.Ldate | log.Lmicroseconds )
	if err != nil {
		fmt.Println(err.Error())
		return 1
	}
	authorized := router.Group("/check", basicAuth(settings.AuthCheck))

	// simplest ping check - is the host up?
	router.GET("/ping/:ip", func(c *gin.Context) {
		request, err := checker.NewRequest(c.Param("ip"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		start := time.Now()
		up, err := request.IsUp()
		duration := time.Since(start)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"request":  request.OriginalAddr,
			"hostport": request.Address,
			"up":       up,
			"timeNS":   duration.Nanoseconds(),
		})
	})

	authorized.GET("/:ip", func(c *gin.Context) {
		request, err := checker.NewRequest(c.Param("ip"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		roll := strings.ToLower(c.Query("version"))
		toPoint := strings.ToLower(c.DefaultQuery("level", "basic"))
		state, err := checker.ParseToState(toPoint)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Unknown check level: '" + toPoint + "'\n"})
			return
		}
		var result checker.CheckResult
		if roll == "" {
			result = request.Check(state, false)
		} else {
			result = request.CheckVersion(state, roll, false)
		}
		if !canSeeOpponentIP(c) { // hide remote IP
			result.Opponent = ""
		}
		switch strings.ToLower(c.Query("pretty")) {
		case "y", "yes", "t", "true", "on", "1":
			c.JSON(http.StatusOK, gin.H{
				"request":  request.OriginalAddr,
				"hostport": request.Address,
				"result":   pretty.MarkupResult(result, allCards),
			})
		default:
			c.JSON(http.StatusOK, gin.H{
				"request":  request.OriginalAddr,
				"hostport": request.Address,
				"result":   result,
			})
		}

	})

	router.GET("/info", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"card-info": settings.CardInfo, "release": settings.Live})
	})

	router.Run(settings.BindAddr)
	return 0
}

func canSeeOpponentIP(c *gin.Context) bool {
	return c.GetBool("CanSeeOp")
}

func basicAuth(checkUrl string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHdr := c.Request.Header.Get("Authorization")
		remoteIP := c.ClientIP()
		if authHdr == "" {
			log.Printf("[Auth] %s - anonymous", remoteIP)
			c.Next()
			return
		}
		if checkUrl == "" {
			respondWithError(500, "Auth proxy is not set; cannot authorise", c)
			return
		}

		request := resty.New().R()
		request.SetHeader("Authorization", authHdr)
		var result struct {
			Id    uint64   `json:"id"`
			Nick  string   `json:"nick"`
			Privs string   `json:"privs"`
			Roles []string `json:"roles"`
		}
		request.SetResult(&result)
		response, err := request.Get(checkUrl)
		if err != nil || !response.IsSuccess() {
			if err != nil {
				log.Printf("[Auth] %s - Backend Auth Proxy Down - %s\n", remoteIP, err.Error())
				respondWithError(500, "Backend Auth Proxy Down", c)
				return
			}
			if response.StatusCode() >= 500 {
				log.Printf("[Auth] %s - Backend Auth Proxy Down? - %s\n", remoteIP, response.Status())
				respondWithError(500, "Backend Auth Proxy Down", c)
				return
			}
			respondWithError(401, "Unauthorized", c)
			return
		}
		canSeeOpponents := false
		for _, role := range result.Roles {
			if role == "see_user_private_hosts" {
				break
			}
		}
		if !canSeeOpponents {
			// super user allowed as a fallback
			canSeeOpponents = result.Privs == "super"
		}
		c.Set("CanSeeOp", canSeeOpponents)
		log.Printf("[Auth] %s - %s (%d) %s VisibleOpponent: %t\n", remoteIP, result.Nick, result.Id, result.Privs, canSeeOpponents)
		c.Next()
	}
}

func respondWithError(code int, message string, c *gin.Context) {
	resp := map[string]string{"error": message}

	c.JSON(code, resp)
	c.Abort()
}

func CliParse() {
	parser := flags.NewParser(&settings, flags.Default)
	args, err := parser.Parse()

	if err != nil {
		switch err.(type) {
		case *flags.Error:
			if err.(*flags.Error).Type == flags.ErrHelp {
				os.Exit(0)
			}
		}
		log.Fatalln(err)
	}
	if len(args) != 0 {
		log.Fatalln("Passed unexpected extra command line arguments, use -h for help")
	}

	if settings.AuthCheck != "" {
		authUrl, err := url.Parse(settings.AuthCheck)
		if err != nil {
			log.Fatalln(err)
		}
		if authUrl.Hostname() == "" {
			log.Fatalf("Cannot parse '%s' as a valid URL", settings.AuthCheck)
		}
		settings.AuthCheck = authUrl.String()
		log.Printf("Attempting to auth requests via %s", settings.AuthCheck)
	}
}
