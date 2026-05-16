package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/vanguard"
	"github.com/joho/godotenv"
	"github.com/pressly/goose/v3"
	"github.com/spf13/cobra"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/meterysh/metery/gen/go/metery/v1/meteryv1connect"
	"github.com/meterysh/metery/internal/auth"
	"github.com/meterysh/metery/internal/service"
	"github.com/meterysh/metery/internal/store"
	"github.com/meterysh/metery/internal/store/migrations"
	"github.com/meterysh/metery/internal/worker"
)

var Version = "dev"

func getDriver(url string) string {
	if strings.HasPrefix(url, "postgres://") || strings.HasPrefix(url, "postgresql://") {
		return "pgx"
	}
	// Default to sqlite for file:, sqlite:, or plain paths
	return "sqlite"
}

func initDB(url string) *sql.DB {
	driver := getDriver(url)
	db, err := sql.Open(driver, url)
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	return db
}

func runMigrations(db *sql.DB, url string) {
	driver := getDriver(url)
	goose.SetBaseFS(migrations.FS)
	dialect := driver
	if driver == "pgx" {
		dialect = "postgres"
	}
	if err := goose.SetDialect(dialect); err != nil {
		log.Fatalf("failed to set goose dialect: %v", err)
	}

	dir := "sqlite"
	if driver == "pgx" {
		dir = "postgres"
	}

	if err := goose.Up(db, dir); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	_ = godotenv.Load()

	var dbUrl string

	rootCmd := &cobra.Command{
		Use:   "metery",
		Short: "Metery - Usage billing and entitlements backend",
	}

	rootCmd.PersistentFlags().StringVar(&dbUrl, "db", getEnvOrDefault("DATABASE_URL", "file:metery.db?cache=shared&mode=rwc"), "Database URL")

	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the API server",
		Run: func(cmd *cobra.Command, args []string) {
			doMigrate, _ := cmd.Flags().GetBool("migrate")
			if !doMigrate {
				envMig := strings.ToLower(os.Getenv("MIGRATE"))
				doMigrate = envMig == "true" || envMig == "1" || envMig == "yes"
			}

			db := initDB(dbUrl)
			defer db.Close()

			if doMigrate {
				runMigrations(db, dbUrl)
			}

			driver := getDriver(dbUrl)
			st := store.New(db, driver)
			srv := service.NewService(st)

			apiKeys := strings.Split(os.Getenv("API_KEYS"), ",")
			authInterceptor := auth.AuthMiddleware(apiKeys)
			opts := connect.WithInterceptors(authInterceptor)

			restOpt := vanguard.WithRESTUnmarshalOptions(vanguard.RESTUnmarshalOptions{
				DiscardUnknownQueryParams: true,
			})
			newSvc := func(pattern string, handler http.Handler) *vanguard.Service {
				return vanguard.NewService(pattern, handler, restOpt)
			}
			services := []*vanguard.Service{
				newSvc(meteryv1connect.NewCustomerServiceHandler(srv, opts)),
				newSvc(meteryv1connect.NewMeterServiceHandler(srv, opts)),
				newSvc(meteryv1connect.NewFeatureServiceHandler(srv, opts)),
				newSvc(meteryv1connect.NewEntitlementServiceHandler(srv, opts)),
				newSvc(meteryv1connect.NewGrantServiceHandler(srv, opts)),
				newSvc(meteryv1connect.NewEventServiceHandler(srv, opts)),
			}

			transcoder, err := vanguard.NewTranscoder(services,
				vanguard.WithCodec(func(res vanguard.TypeResolver) vanguard.Codec {
					codec := vanguard.NewJSONCodec(res)
					codec.MarshalOptions.UseProtoNames = true
					codec.MarshalOptions.EmitUnpopulated = true
					codec.UnmarshalOptions.DiscardUnknown = true
					return codec
				}),
			)
			if err != nil {
				log.Fatalf("failed to initialize vanguard transcoder: %v", err)
			}

			hostname := getEnvOrDefault("HOSTNAME", "http://localhost:8080")

			mux := http.NewServeMux()
			mux.Handle("/", transcoder)
			mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("ok"))
			})
			mux.HandleFunc("/worker/run", func(w http.ResponseWriter, r *http.Request) {
				worker.RunOnce(r.Context(), st)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("{\"status\":\"ok\"}"))
			})

			publicFS := http.FileServer(http.Dir("public"))
			if spec, err := os.ReadFile("public/openapi.yaml"); err == nil {
				spec = bytes.ReplaceAll(spec, []byte("https://metery.example.com"), []byte(hostname))
				mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
					w.Write(spec)
				})
			}
			if entries, err := os.ReadDir("public"); err == nil {
				for _, e := range entries {
					if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") && e.Name() != "openapi.yaml" {
						mux.Handle("GET /"+e.Name(), publicFS)
					}
				}
			}

			h2cSrv := &http2.Server{}
			httpSrv := &http.Server{
				Addr:    ":8080",
				Handler: h2c.NewHandler(mux, h2cSrv),
			}

			go func() {
				log.Println("Listening on :8080")
				if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Fatalf("server failed: %v", err)
				}
			}()

			quit := make(chan os.Signal, 1)
			signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
			<-quit
			log.Println("Shutting down server...")

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := httpSrv.Shutdown(ctx); err != nil {
				log.Fatalf("server forced to shutdown: %v", err)
			}
		},
	}
	serveCmd.Flags().Bool("migrate", false, "Run migrations before starting")

	migrateCmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run database migrations",
		Run: func(cmd *cobra.Command, args []string) {
			db := initDB(dbUrl)
			defer db.Close()
			runMigrations(db, dbUrl)
			log.Println("Migrations completed successfully")
		},
	}

	workerCmd := &cobra.Command{
		Use:   "worker",
		Short: "Run the recurrence worker",
		Run: func(cmd *cobra.Command, args []string) {
			db := initDB(dbUrl)
			defer db.Close()
			driver := getDriver(dbUrl)
			st := store.New(db, driver)

			log.Println("Starting recurrence worker...")
			ctx, cancel := context.WithCancel(context.Background())

			go worker.RunRecurrenceWorker(ctx, st)

			quit := make(chan os.Signal, 1)
			signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
			<-quit
			log.Println("Shutting down worker...")
			cancel()
		},
	}

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version number",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("metery version %s\n", Version)
		},
	}

	rootCmd.AddCommand(serveCmd, migrateCmd, workerCmd, versionCmd)
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
