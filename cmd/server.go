package cmd

import (
	"bytes"
	"fmt"
	"image/png"

	// "html/template"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/chikamim/nilsimsa"
	"github.com/corona10/goimagehash"
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
	"github.com/sensepost/gowitness/lib"
	"github.com/sensepost/gowitness/storage"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

// defaultMaxJobs is how many screenshots may run concurrently by default.
const defaultMaxJobs = 5

var (
	rsDB    *gorm.DB
	theme   string = "dark" // or light
	maxJobs int
)

// serverCmd represents the server command
var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Starts a webserver that serves the report interface, api and screenshot tool",
	Long: `Starts a webserver that serves the report interface, api and screenshot tool.

The report server is available in the root path, aka /.
The API is available from the /api path.

The global database and screenshot paths should be set to the same as
what they were when a scan was run. The report server also has the ability
to screenshot ad-hoc URLs provided to the submission page.

The API is usable to take screenshots and reflect them back amongst other useful things.
Most of the Gowitness core is exposed via the API.

NOTE: When changing the server address to something other than localhost, make 
sure that only authorised connections can be made to the server port. By default,
access is restricted to localhost to reduce the risk of SSRF attacks against the
host or hosting infrastructure (AWS/Azure/GCP, etc). Consider strict IP filtering
or fronting this server with an authentication aware reverse proxy.

Allowed URLs, by default, need to start with http:// or https://. If you need
this restriction lifted, add the --allow-insecure-uri / -A flag. A word of 
warning though, that also means that someone may request a URL like file:///etc/passwd.
`,
	Example: `$ gowitness server
$ gowitness server --address 0.0.0.0:8080
$ gowitness server --address 127.0.0.1:9000 --allow-insecure-uri`,
	Run: func(cmd *cobra.Command, args []string) {
		log := options.Logger

		if !strings.Contains(options.ServerAddr, "localhost") {
			log.Warn().Msg("exposing this server to other networks is dangerous! see the server command help for more information")
		}

		if !strings.HasPrefix(options.BasePath, "/") {
			log.Warn().Msg("base path does not start with a /")
		}

		// db
		dbh, err := db.Get()
		if err != nil {
			log.Fatal().Err(err).Msg("could not gt db handle")
		}
		rsDB = dbh

		log.Info().Str("path", db.Location).Msg("db path")
		log.Info().Str("path", options.ScreenshotPath).Msg("screenshot path")

		if options.Debug {
			gin.SetMode(gin.DebugMode)
		} else {
			gin.SetMode(gin.ReleaseMode)
		}

		if maxJobs < 1 {
			log.Warn().Int("max-jobs", maxJobs).Int("using", defaultMaxJobs).
				Msg("max-jobs must be at least 1, falling back to the default")
			maxJobs = defaultMaxJobs
		}

		// make sure the screenshot directory exists and is writable up front,
		// rather than discovering it once every screenshot has already been taken
		if err := options.PrepareScreenshotPath(); err != nil {
			log.Fatal().Err(err).Str("path", options.ScreenshotPath).
				Msg("screenshot path is not usable")
		}

		reclaimOrphanedJobs(rsDB, log)

		// The queue worker polls for pending jobs and runs at most maxJobs
		// screenshots concurrently. SkipIfStillRunning keeps a slow tick from
		// overlapping with the next one and double-claiming jobs.
		c := cron.New(cron.WithChain(cron.SkipIfStillRunning(cron.DiscardLogger)))
		if _, err := c.AddFunc("@every 30s", func() { dispatchQueue(rsDB, log) }); err != nil {
			log.Fatal().Err(err).Msg("could not schedule the screenshot queue worker")
		}
		c.Start()
		defer c.Stop()

		r := gin.Default()
		r.Use(cors.New(cors.Config{
			AllowOrigins: []string{"*"},
			AllowHeaders: []string{"Origin", "Content-type"},
		}))
		// r.Use(themeChooser(&theme))

		// add / suffix to the base url so that we can be certain about
		// the trim in the template helper
		if !strings.HasSuffix(options.BasePath, "/") {
			options.BasePath += "/"
		}

		log.Info().Str("base-path", options.BasePath).Msg("basepath")

		// r.GET("/submit", getSubmitHandler)
		// // r.GET("/truncate", getTruncateHandler)
		// r.POST("/submit", submitHandler)
		// r.POST("/search", searchHandler)

		// // static assets & raw screenshot files
		assetFs, err := fs.Sub(Embedded, "web/dist/assets")
		if err != nil {
			log.Fatal().Err(err).Msg("could not fs.Sub Assets")
		}

		// // assets & screenshots
		r.StaticFS("/assets/", http.FS(assetFs))
		r.StaticFS("/screenshots", http.Dir(options.ScreenshotPath))

		// dist, _ := fs.Sub(Embedded, "web/dist")
		// r.StaticFS("/", http.FS(dist))

		r.Use(static.Serve("/", static.LocalFile("web/dist", false)))
		r.Use(static.Serve("/gallery", static.LocalFile("web/dist", false)))
		r.Use(static.Serve("/table", static.LocalFile("web/dist", false)))
		r.Use(static.Serve("/submit", static.LocalFile("web/dist", false)))
		r.Use(static.Serve("/log", static.LocalFile("web/dist", false)))
		// json api routes
		api := r.Group("/api")
		{
			// api.GET("/test", apiURLHandler2)
			// For web

			api.GET("/statistic", apiStatisticHandler)
			api.GET("/log", apiLogHandler)
			api.GET("/gallery", apiGalleryHandler)
			api.GET("/table", apiTableHandler)

			// For other system
			api.POST("/url/hidden", apiUrlHiddenHandler)
			api.GET("/list", apiURLHandler)
			api.POST("/config/add", apiAddConfigHandler)
			api.GET("/config/get", apiGetConfigHandler)
			api.POST("/config/set", apiSetConfigHandler)
			api.POST("/config/delete", apiDeleteConfigHandler)
			api.GET("/search", apiSearchHandler)
			api.GET("/detail/:id", apiDetailHandler)
			api.GET("/detail/:id/screenshot", apiDetailScreenshotHandler)
			api.POST("/screenshot", apiScreenshotHandler)
			api.POST("/screenshot/v2", apiScreenshotHandlerV2) // screenshot with queue
			api.POST("/screenshot/v3", apiScreenshotHandlerV3)
			api.GET("/samesite/check", apiCheckSameSite)
			api.GET("/samesite/hash", apiHashSameSite)
		}

		log.Info().Str("address", options.ServerAddr).Msg("server listening")
		if err := r.Run(options.ServerAddr); err != nil {
			log.Fatal().Err(err).Msg("webserver failed")
		}
	},
}

func init() {
	rootCmd.AddCommand(serverCmd)

	serverCmd.Flags().StringVarP(&options.ServerAddr, "address", "a", "localhost:7171", "server listening address")
	serverCmd.Flags().BoolVarP(&options.AllowInsecureURIs, "allow-insecure-uri", "A", false, "allow uris that dont start with http(s)")
	serverCmd.Flags().StringVarP(&options.BasePath, "base-path", "b", "/", "set the servers base path (useful for some reverse proxy setups)")
	serverCmd.Flags().IntVarP(&maxJobs, "max-jobs", "m", defaultMaxJobs, "maximum number of concurrent screenshot jobs")
}

// reclaimOrphanedJobs resolves jobs still marked as running at startup, since no
// worker of this process owns them. A job whose URL already has a stored result
// completed successfully, so it is marked done; anything else is requeued.
func reclaimOrphanedJobs(db *gorm.DB, log *zerolog.Logger) {
	completed := db.Model(&storage.ScreenshotQueue{}).
		Where("p_id = ?", storage.QueueRunning).
		Where("EXISTS (SELECT 1 FROM urls WHERE urls.url = screenshot_queues.url AND urls.deleted_at IS NULL)").
		Update("p_id", storage.QueueDone)
	if completed.Error != nil {
		log.Error().Err(completed.Error).Msg("could not reconcile completed screenshot jobs")
		return
	}

	requeued := db.Model(&storage.ScreenshotQueue{}).
		Where("p_id = ?", storage.QueueRunning).
		Update("p_id", storage.QueuePending)
	if requeued.Error != nil {
		log.Error().Err(requeued.Error).Msg("could not requeue orphaned screenshot jobs")
		return
	}

	if completed.RowsAffected > 0 || requeued.RowsAffected > 0 {
		log.Info().Int64("completed", completed.RowsAffected).Int64("requeued", requeued.RowsAffected).
			Msg("reconciled screenshot jobs left running by a previous run")
	}
}

// dispatchQueue claims up to the number of free worker slots worth of pending
// jobs and screenshots them in the background. It returns as soon as the jobs
// are launched; in-flight jobs stay marked QueueRunning, so the next tick sees
// them when working out how many slots are free.
func dispatchQueue(db *gorm.DB, log *zerolog.Logger) {
	var running int64
	if err := db.Model(&storage.ScreenshotQueue{}).
		Where("p_id = ?", storage.QueueRunning).Count(&running).Error; err != nil {
		log.Error().Err(err).Msg("could not count running screenshot jobs")
		return
	}

	free := int64(maxJobs) - running
	if free < 1 {
		return
	}

	var pending []storage.ScreenshotQueue
	if err := db.Where("p_id = ?", storage.QueuePending).
		Order("id asc").Limit(int(free)).Find(&pending).Error; err != nil {
		log.Error().Err(err).Msg("could not read pending screenshot jobs")
		return
	}

	for _, row := range pending {
		targetURL, err := url.Parse(row.URL)
		if err != nil {
			db.Model(&storage.ScreenshotQueue{}).Where("id = ?", row.ID).
				Updates(map[string]any{"p_id": storage.QueueFailed, "error_msg": "invalid url - " + err.Error()})
			continue
		}

		// claim the job, guarding on it still being pending so that a job is
		// never handed to two workers
		claim := db.Model(&storage.ScreenshotQueue{}).
			Where("id = ? AND p_id = ?", row.ID, storage.QueuePending).
			Update("p_id", storage.QueueRunning)
		if claim.Error != nil {
			log.Error().Err(claim.Error).Uint("job", row.ID).Msg("could not claim screenshot job")
			continue
		}
		if claim.RowsAffected == 0 {
			continue
		}

		log.Info().Uint("job", row.ID).Str("url", row.URL).Msg("screenshotting queued url")

		go func(u *url.URL, qid uint, idURL int, callback string) {
			defer func() {
				if r := recover(); r != nil {
					log.Error().Interface("panic", r).Uint("job", qid).Msg("screenshot job panicked")
					db.Model(&storage.ScreenshotQueue{}).Where("id = ?", qid).
						Updates(map[string]any{"p_id": storage.QueueFailed, "error_msg": fmt.Sprintf("panic: %v", r)})
				}
			}()

			p := &lib.Processor{
				Logger:         options.Logger,
				Db:             db,
				Chrome:         chrm,
				URL:            u,
				QID:            qid,
				IdUrl:          idURL,
				Callback:       callback,
				ScreenshotPath: options.ScreenshotPath,
			}

			p.Gowitness()
		}(targetURL, row.ID, row.IdUrl, row.Callback)
	}
}

// middleware
// --

// getTheme gets the current theme choice
func getTheme() string {
	return theme
}

// themeChooser is a middleware to set the theme to use in the base template
func themeChooser(choice *string) gin.HandlerFunc {
	return func(c *gin.Context) {

		// parse the query string as preference. this will indicate a theme switch
		q := c.Query("theme")
		if q == "light" {
			d := "light"
			*choice = d

			// set the cookie for next time
			c.SetCookie("gowitness_theme", "light", 604800, "/", "", false, false)
			return
		}

		if q == "dark" {
			d := "dark"
			*choice = d

			// set the cookie for next time
			c.SetCookie("gowitness_theme", "dark", 604800, "/", "", false, false)
			return
		}

		// if ?theme was invalid, read the cookie value.

		cookie, err := c.Cookie("gowitness_theme")
		if err != nil {
			d := "dark"
			*choice = d

			// set the cookie for next time
			c.SetCookie("gowitness_theme", "dark", 604800, "/", "", false, false)
			return
		}

		if cookie == "light" {
			d := "light"
			*choice = d
		}

		if cookie == "dark" {
			d := "dark"
			*choice = d
		}
	}
}

// getSubmitHandler handles generating the view to submit urls
func getSubmitHandler(c *gin.Context) {
	c.HTML(http.StatusOK, "submit.html", nil)
}

func getTruncateHandler(c *gin.Context) {
	// rsDB.Exec("TRUNCATE TABLE screenshot_queues,network_logs,console_logs,technologies,tls_certificate_dns_names,tls_certificates,tls,headers,urls")
	rsDB.Exec("DELETE FROM  screenshot_queues")
	rsDB.Exec("DELETE FROM  network_logs")
	rsDB.Exec("DELETE FROM  console_logs")
	rsDB.Exec("DELETE FROM  technologies")
	rsDB.Exec("DELETE FROM  tls_certificate_dns_names")
	rsDB.Exec("DELETE FROM  tls_certificates")
	rsDB.Exec("DELETE FROM tls")
	rsDB.Exec("DELETE FROM headers")
	rsDB.Exec("DELETE FROM urls")
	os.RemoveAll("screenshots")
	c.Redirect(http.StatusMovedPermanently, "/")
}

// submitHandler handles url submissions
func submitHandler(c *gin.Context) {

	// prepare target
	url, err := url.Parse(strings.TrimSpace(c.PostForm("url")))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	if !options.AllowInsecureURIs {
		if !strings.HasPrefix(url.Scheme, "http") {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status":  "error",
				"message": "only http(s) urls are accepted",
			})
			return
		}
	}

	fn := lib.SafeFileName(url.String())
	fp := lib.ScreenshotPath(fn, url, options.ScreenshotPath)

	preflight, err := chrm.Preflight(url)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	result, err := chrm.Screenshot(url)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	var rid uint
	if rsDB != nil {
		if rid, err = chrm.StoreRequest(rsDB, preflight, result, fn, 0, ""); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status":  "error",
				"message": err.Error(),
			})
			return
		}
	}

	if err := os.WriteFile(fp, result.Screenshot, 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	if rid > 0 {
		c.Redirect(http.StatusMovedPermanently, "/details/"+strconv.Itoa(int(rid)))
		return
	}

	c.Redirect(http.StatusMovedPermanently, "/submit")
}

// searchHandler handles report searching
func searchHandler(c *gin.Context) {

	query := c.PostForm("search_query")

	if query == "" {
		c.HTML(http.StatusOK, "search.html", nil)
		return
	}

	// sql friendly string search
	search := "%" + query + "%"

	// urls
	var urls []storage.URL
	rsDB.
		Where("URL LIKE ?", search).
		Or("Title LIKE ?", search).
		Or("DOM LIKE ?", search).
		Find(&urls)

	// urgh, for these relations it seems like we need to count
	// and then select? :|

	// technologies
	var technologies []storage.URL
	var technologiesCount int64
	rsDB.Model(storage.Technologie{}).Where("Value LIKE ?", search).Count(&technologiesCount)
	if technologiesCount > 0 {
		rsDB.Preload("Technologies", "Value LIKE ?", search).Find(&technologies)
	}

	// headers
	var headers []storage.URL
	var headersCount int64
	rsDB.Model(storage.Header{}).Where("Key LIKE ? OR Value LIKE ?", search, search).Count(&headersCount)
	if headersCount > 0 {
		rsDB.Preload("Headers", "Key LIKE ? OR Value LIKE ?", search, search).Find(&headers)
	}

	// console logs
	var console []storage.URL
	var consoleCount int64
	rsDB.Model(storage.ConsoleLog{}).Where("Type LIKE ? OR Value LIKE ?", search, search).Count(&consoleCount)
	if consoleCount > 0 {
		rsDB.Preload("Console", "Type LIKE ? OR Value LIKE ?", search, search).Find(&console)
	}

	// network logs
	var network []storage.URL
	var networkCount int64
	rsDB.Model(storage.NetworkLog{}).Where("URL LIKE ? OR IP LIKE ? OR Error LIKE ?", search, search, search).Count(&networkCount)
	if networkCount > 0 {
		rsDB.Preload("Network", "URL LIKE ? OR IP LIKE ? OR Error LIKE ?", search, search, search).Find(&network)
	}

	c.HTML(http.StatusOK, "search.html", gin.H{
		"Term":         query,
		"URLS":         urls,
		"Tech":         technologies,
		"TechCount":    technologiesCount,
		"Headers":      headers,
		"HeadersCount": headersCount,
		"Console":      console,
		"ConsoleCount": consoleCount,
		"Network":      network,
		"NetworkCount": networkCount,
	})
}

// getPageLimit gets the limit and page query string values from a request
func getPageLimit(c *gin.Context) (page int, limit int, err error) {

	pageS := strings.TrimSpace(c.Query("page"))
	limitS := strings.TrimSpace(c.Query("limit"))

	if pageS == "" {
		pageS = "-1"
	}
	if limitS == "" {
		limitS = "0"
	}

	page, err = strconv.Atoi(pageS)
	if err != nil {
		return
	}
	limit, err = strconv.Atoi(limitS)
	if err != nil {
		return
	}

	return
}

func checkHash(hash1, hash2 string, htype int) bool {
	// Implement your hash checking logic here
	// For now, let's just compare the hashes directly
	if hash1 == hash2 {
		return true
	}
	if hash1 == "" || hash2 == "" {
		return false
	}
	if htype == 1 {
		return nilsimsa.DiffHexScore(hash1, hash2) > 0.6
	} else if htype == 2 {
		hash1, _ := strconv.ParseUint(hash1, 10, 64)
		hash2, _ := strconv.ParseUint(hash2, 10, 64)
		newImageHash := goimagehash.NewImageHash(hash1, 2)
		newImageHash2 := goimagehash.NewImageHash(hash2, 2)
		distance, _ := newImageHash.Distance(newImageHash2)
		return distance < 20
	} else {
		return false
	}

}

// API request handlers follow here
// --
func apiCheckSameSite(c *gin.Context) {
	var urls []storage.URL
	var sameUrls []storage.URL
	// var sameHashs []storage.SameHash

	rsDB.Select("ID", "URL", "PerceptionHash", "NilsimsaHash", "IdUrl", "SameSite").Where("same_site = ?", 0).Find(&urls)
	rsDB.Select("ID", "PerceptionHash", "NilsimsaHash").Where("same_site = ID").Find(&sameUrls)
	if len(sameUrls) == 0 && len(urls) != 0 {
		rsDB.Model(&storage.URL{}).Where("ID = ?", urls[0].ID).Update("SameSite", urls[0].ID)
		rsDB.Select("ID", "PerceptionHash", "NilsimsaHash").Where("same_site = ID").Find(&sameUrls)
	}

	for _, url := range urls {
		checksamehash := false
		for _, shash := range sameUrls {
			if checkHash(shash.NilsimsaHash, url.NilsimsaHash, 1) && checkHash(shash.PerceptionHash, url.PerceptionHash, 2) {
				rsDB.Model(&storage.URL{}).Where("ID = ?", url.ID).Update("SameSite", shash.ID)
				checksamehash = true
				break
			}
		}
		if !checksamehash {
			rsDB.Model(&storage.URL{}).Where("ID = ?", url.ID).Update("SameSite", url.ID)
			rsDB.Select("ID", "PerceptionHash", "NilsimsaHash").Where("same_site = ID").Find(&sameUrls)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
	})

}

func apiHashSameSite(c *gin.Context) {
	var urls []storage.URL
	var NilsimsaHash string
	rsDB.Select("ID", "PerceptionHash", "NilsimsaHash", "DOM").Where("perception_hash IS NULL").Or("nilsimsa_hash IS NULL").Find(&urls)
	for _, url := range urls {
		if url.NilsimsaHash == "" && url.DOM != "" {
			NilsimsaHash = nilsimsa.HexSum([]byte(url.DOM))
			var dburl storage.URL
			rsDB.First(&dburl, url.ID)
			dburl.NilsimsaHash = NilsimsaHash
			rsDB.Save(&dburl)
		}
		if url.PerceptionHash == "" && url.Filename != "" {
			p := options.ScreenshotPath + "/" + url.Filename
			screenshot, err := os.ReadFile(p)
			if err != nil {
				continue
			}

			img, err := png.Decode(bytes.NewReader(screenshot))
			if err != nil {
				continue
			}

			comp, err := goimagehash.PerceptionHash(img)
			if err != nil {
				continue
			}
			var dburl storage.URL
			rsDB.First(&dburl, url.ID)
			dburl.PerceptionHash = comp.ToString()
			rsDB.Save(&dburl)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
	})
}

func apiUrlHiddenHandler(c *gin.Context) {
	type Request struct {
		Id     int  `json:"id"`
		Hidden bool `json:"hidden"`
	}
	var requestData Request
	if err := c.ShouldBindJSON(&requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	if err := rsDB.Model(&storage.URL{}).Where("ID = ?", requestData.Id).Update("Hidden", requestData.Hidden); err.Error != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": false,
			"error":  err.Error,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
	})
}

func apiStatisticHandler(c *gin.Context) {

	// get the sqlite db size
	var size int64
	rsDB.Raw("SELECT page_count * page_size as size FROM pragma_page_count(), pragma_page_size();").Take(&size)

	// count some statistics

	var urlCount int64
	rsDB.Model(&storage.URL{}).Count(&urlCount)

	var certCount int64
	rsDB.Model(&storage.TLS{}).Count(&certCount)

	var certDNSNameCount int64
	rsDB.Model(&storage.TLSCertificateDNSName{}).Count(&certDNSNameCount)

	var headerCount int64
	rsDB.Model(&storage.Header{}).Count(&headerCount)

	var techCount int64
	rsDB.Model(&storage.Technologie{}).Distinct().Count(&techCount)

	c.JSON(http.StatusOK, gin.H{
		"DBSzie":       fmt.Sprintf("%.2f", float64(size)/1e6),
		"URLCount":     urlCount,
		"CertCount":    certCount,
		"DNSNameCount": certDNSNameCount,
		"HeaderCount":  headerCount,
		"TechCount":    techCount,
	})
}

func apiTableHandler(c *gin.Context) {

	var urls []storage.URL
	rsDB.Preload("Network").Preload("Console").Preload("Technologies").Find(&urls)

	c.JSON(http.StatusOK, gin.H{
		"data": urls,
	})
}

func apiGalleryHandler(c *gin.Context) {

	currPage, limit, err := getPageLimit(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	pager := &lib.Pagination{
		DB:       rsDB,
		CurrPage: currPage,
		Limit:    limit,
	}

	// perception hashing
	if strings.TrimSpace(c.Query("perception_sort")) == "true" {
		pager.OrderBy = []string{"perception_hash desc"}
	} else {
		pager.OrderBy = []string{"id desc"}
	}

	// Khong show hidden
	if strings.TrimSpace(c.Query("hidden")) == "true" {
		pager.Hidden = true
	} else {
		pager.Hidden = false
	}

	if strings.TrimSpace(c.Query("samesite")) == "true" {
		pager.Samesite = true
	} else {
		pager.Samesite = false
	}

	var urls []storage.URL
	page, err := pager.Page(&urls)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": page,
	})
}

// apiURLHandler returns the list of URLS in the database
func apiURLHandler(c *gin.Context) {

	// use gorm SmartSelect Fields to filter URL
	type apiURL struct {
		ID           uint64
		URL          string
		FinalURL     string
		ResponseCode int
		Title        string
	}

	var urls []apiURL
	rsDB.Model(&storage.URL{}).Find(&urls)

	c.JSON(http.StatusOK, urls)
}

// apiSearchHandler allows for searches via the api
func apiSearchHandler(c *gin.Context) {

	query := c.Query("q")

	if query == "" {
		c.JSON(http.StatusNotAcceptable, gin.H{
			"status": "error",
			"error":  "search parameter 'q' empty",
		})
		return
	}

	// use gorm SmartSelect Fields to filter URL
	search := "%" + query + "%"
	var urls []storage.URL

	rsDB.
		Where("URL LIKE ?", search).
		Or("Title LIKE ?", search).
		Or("DOM LIKE ?", search).
		Find(&urls)

	c.JSON(http.StatusOK, urls)
}

// apiDetailHandler handles a detail request for screenshot information
func apiDetailHandler(c *gin.Context) {

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	var url storage.URL
	rsDB.
		Preload("Headers").
		Preload("TLS").
		Preload("TLS.TLSCertificates").
		Preload("TLS.TLSCertificates.DNSNames").
		Preload("Technologies").
		Preload("Console").
		Preload("Network", func(db *gorm.DB) *gorm.DB {
			db = db.Order("Time asc")
			return db
		}).
		First(&url, id)

	// get pagination limits
	var max uint
	rsDB.Model(storage.URL{}).Select("max(id)").First(&max)

	previous := url.ID
	next := url.ID

	if previous > 0 {
		previous = previous - 1
	}

	if next < max {
		next = next + 1
	}

	c.JSON(http.StatusOK, gin.H{
		"ID":       id,
		"Data":     url,
		"Previous": previous,
		"Next":     next,
		"Max":      max,
	})
}

// apiDetailScreenshotHandler serves the screenshot for a specific url id
func apiDetailScreenshotHandler(c *gin.Context) {
	var url storage.URL
	rsDB.First(&url, c.Param("id"))

	if url.ID == 0 {
		c.JSON(http.StatusNotFound, nil)
		return
	}

	p := options.ScreenshotPath + "/" + url.Filename

	screenshot, err := os.ReadFile(p)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"stauts": "errir",
			"error":  err.Error(),
		})
	}

	c.Data(http.StatusOK, "image/png", screenshot)
}

// apiScreenshot takes a screenshot of a URL
func apiScreenshotHandler(c *gin.Context) {

	type Request struct {
		URL     string   `json:"url"`
		Headers []string `json:"headers"`
		// set oneshot to "true" if you just want to see the screenshot, and not add it to the report
		OneShot string `json:"oneshot"`
	}

	var requestData Request
	if err := c.ShouldBindJSON(&requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	targetURL, err := url.Parse(requestData.URL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	if !options.AllowInsecureURIs {
		if !strings.HasPrefix(targetURL.Scheme, "http") {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status":  "error",
				"message": "only http(s) urls are accepted",
			})
			return
		}
	}

	// prepare request headers
	if len(requestData.Headers) > 0 {
		chrm.Headers = requestData.Headers
	}
	chrm.PrepareHeaderMap()

	// deliver a oneshot screenshot to the user
	if requestData.OneShot == "true" {
		result, err := chrm.Screenshot(targetURL)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status":  "error",
				"message": err.Error(),
			})
			return
		}

		c.Data(http.StatusOK, "image/png", result.Screenshot)
		return
	}

	// queue a fetch session for the url
	if err = options.PrepareScreenshotPath(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	go func(u *url.URL) {
		p := &lib.Processor{
			Logger:         options.Logger,
			Db:             rsDB,
			Chrome:         chrm,
			URL:            u,
			ScreenshotPath: options.ScreenshotPath,
		}

		p.Gowitness()
	}(targetURL)

	c.JSON(http.StatusCreated, gin.H{
		"status": "created",
	})
}

// apiScreenshot takes a screenshot of a URL
func apiScreenshotHandlerV2(c *gin.Context) {

	type Request struct {
		URL      string   `json:"url"`
		Headers  []string `json:"headers"`
		URLS     []string `json:"urls"`
		Callback string   `json:"callback"`
		IdUrl    int      `json:"idUrl"`
	}

	var requestData Request
	if err := c.ShouldBindJSON(&requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	var c_urls []string
	if requestData.URL != "" {
		c_urls = append(c_urls, requestData.URL)
	} else if len(requestData.URLS) != 0 {
		c_urls = requestData.URLS
	} else {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "Empty input",
		})
		return
	}

	if err := options.PrepareScreenshotPath(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	var errorsMsg []string

	for _, c_url := range c_urls {
		targetURL, err := url.Parse(c_url)
		if err != nil {
			errorsMsg = append(errorsMsg, err.Error())
			continue
		}

		if !options.AllowInsecureURIs {
			if !strings.HasPrefix(targetURL.Scheme, "http") {
				errorsMsg = append(errorsMsg, "Not AllowInsecureURIs")
				continue
			}
		}

		screenshot_queue := storage.ScreenshotQueue{URL: targetURL.String(), PID: storage.QueuePending, Callback: requestData.Callback, IdUrl: requestData.IdUrl}

		result := rsDB.Create(&screenshot_queue)
		if result.Error != nil {
			errorsMsg = append(errorsMsg, result.Error.Error())
			continue
		}
	}

	if len(errorsMsg) != 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  errorsMsg,
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status": "created ",
	})
}

func apiScreenshotHandlerV3(c *gin.Context) {
	type Urls struct {
		Name string `json:"name"`
		Id   int    `json:"id"`
	}

	type Request struct {
		Headers  []string `json:"headers"`
		URLS     []Urls   `json:"urls"`
		Callback string   `json:"callback"`
	}

	var requestData Request
	if err := c.ShouldBindJSON(&requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	if err := options.PrepareScreenshotPath(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	var errorsMsg []string

	for _, c_url := range requestData.URLS {
		targetURL, err := url.Parse(c_url.Name)
		if err != nil {
			errorsMsg = append(errorsMsg, err.Error())
			continue
		}

		if !options.AllowInsecureURIs {
			if !strings.HasPrefix(targetURL.Scheme, "http") {
				errorsMsg = append(errorsMsg, "Not AllowInsecureURIs")
				continue
			}
		}

		screenshot_queue := storage.ScreenshotQueue{URL: targetURL.String(), PID: storage.QueuePending, Callback: requestData.Callback, IdUrl: c_url.Id}

		result := rsDB.Create(&screenshot_queue)
		if result.Error != nil {
			errorsMsg = append(errorsMsg, result.Error.Error())
			continue
		}
	}

	if len(errorsMsg) != 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  errorsMsg,
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status": "created ",
	})
}

func apiLogHandler(c *gin.Context) {
	var urls []storage.ScreenshotQueue
	rsDB.Where("p_id = ?", -1).Find(&urls)

	c.JSON(http.StatusOK, gin.H{
		"status": true,
		"data":   urls,
	})
}

func apiGetConfigHandler(c *gin.Context) {
	var config []storage.ConfigMachine
	rsDB.Find(&config)

	c.JSON(http.StatusOK, gin.H{
		"data": config,
	})
}

func apiAddConfigHandler(c *gin.Context) {
	type Request struct {
		Key     string `json:"key"`
		Machine string `json:"machine"`
		Value   string `json:"value"`
	}
	var requestData Request
	if err := c.ShouldBindJSON(&requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	if err := rsDB.Create(&storage.ConfigMachine{Key: requestData.Key, Machine: requestData.Machine, Value: requestData.Value}); err.Error != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": false,
			"error":  err.Error,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
	})
}

func apiSetConfigHandler(c *gin.Context) {
	type Request struct {
		Id      int    `json:"id"`
		Key     string `json:"key"`
		Machine string `json:"machine"`
		Value   string `json:"value"`
	}
	var requestData Request
	if err := c.ShouldBindJSON(&requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	if err := rsDB.Model(&storage.ConfigMachine{}).Where("ID = ?", requestData.Id).Update("Value", requestData.Value).Update("Machine", requestData.Machine); err.Error != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": false,
			"error":  err.Error,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
	})
}

func apiDeleteConfigHandler(c *gin.Context) {
	type Request struct {
		Id int `json:"id"`
	}
	var requestData Request
	if err := c.ShouldBindJSON(&requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	if err := rsDB.Delete(&storage.ConfigMachine{}, requestData.Id); err.Error != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  err.Error,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": true,
	})
}

func apiURLHandler2(c *gin.Context) {
	var urls []storage.ScreenshotQueue
	rsDB.Where("p_id = ?", -1).Find(&urls)

	// var count int64
	// rsDB.Model(&storage.ScreenshotQueue{}).Where("p_id > 0").Count(&count)
	// fmt.Println(count)
	// rsDB.Model(&storage.ScreenshotQueue{}).Where("ID = ?", 1).Update("PID", 1)
	// var st_sc []storage.ScreenshotQueue
	// rsDB.Find(&st_sc)
	// for _, row := range st_sc {
	// 	fmt.Println("values: ", row.ID, row.URL)
	// }
	// c.JSON(http.StatusCreated, gin.H{
	// 	"status": st_sc,
	// })
	c.JSON(http.StatusOK, gin.H{
		"status": true,
		"data":   urls,
	})
}
