package main

import (
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

var dbUrl string

func getDriver(url string) string {
	if strings.HasPrefix(url, "postgres://") || strings.HasPrefix(url, "postgresql://") {
		return "pgx"
	}
	// Default to sqlite for file:, sqlite:, or plain paths
	return "sqlite"
}

func initDB() *sql.DB {
	driver := getDriver(dbUrl)
	db, err := sql.Open(driver, dbUrl)
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	return db
}

func runMigrations(db *sql.DB) {
	driver := getDriver(dbUrl)
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect(driver); err != nil {
		log.Fatalf("failed to set goose dialect: %v", err)
	}

	dir := "sqlite"
	if driver == "pgx" {
		dir = "pgx"
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

			db := initDB()
			defer db.Close()

			if doMigrate {
				runMigrations(db)
			}

			driver := getDriver(dbUrl)
			st := store.New(db, driver)
			srv := service.NewService(st)

			apiKeys := strings.Split(os.Getenv("API_KEYS"), ",")
			authInterceptor := auth.AuthMiddleware(apiKeys)
			opts := connect.WithInterceptors(authInterceptor)

			services := []*vanguard.Service{
				vanguard.NewService(meteryv1connect.NewCustomerServiceHandler(srv, opts)),
				vanguard.NewService(meteryv1connect.NewMeterServiceHandler(srv, opts)),
				vanguard.NewService(meteryv1connect.NewFeatureServiceHandler(srv, opts)),
				vanguard.NewService(meteryv1connect.NewEntitlementServiceHandler(srv, opts)),
				vanguard.NewService(meteryv1connect.NewGrantServiceHandler(srv, opts)),
				vanguard.NewService(meteryv1connect.NewEventServiceHandler(srv, opts)),
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

			mux := http.NewServeMux()
			mux.Handle("/", transcoder)
			mux.HandleFunc("/worker/run", func(w http.ResponseWriter, r *http.Request) {
				worker.RunOnce(r.Context(), st)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("{\"status\":\"ok\"}"))
			})

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
			db := initDB()
			defer db.Close()
			runMigrations(db)
			log.Println("Migrations completed successfully")
		},
	}

	workerCmd := &cobra.Command{
		Use:   "worker",
		Short: "Run the recurrence worker",
		Run: func(cmd *cobra.Command, args []string) {
			db := initDB()
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
			fmt.Println("metery v0.1.0")
		},
	}

	rootCmd.AddCommand(serveCmd, migrateCmd, workerCmd, versionCmd)
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
