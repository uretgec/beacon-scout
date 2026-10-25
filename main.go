package main

import (
	"embed"

	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.
// See https://pkg.go.dev/embed for more information.

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	// Register a custom event whose associated data type is string.
	// This is not required, but the binding generator will pick up registered events
	// and provide a strongly typed JS/TS API for them.
	application.RegisterEvent[BeaconListEvent]("beacons:list")
}

// main function serves as the application's entry point. It initializes the application, creates a window,
// and starts a goroutine that emits a time-based event every second. It subsequently runs the application and
// logs any error that might occur.
func main() {

	// Create a new Wails application by providing the necessary options.
	// Variables 'Name' and 'Description' are for application metadata.
	// 'Assets' configures the asset server with the 'FS' variable pointing to the frontend files.
	// 'Bind' is a list of Go struct instances. The frontend has access to the methods of these instances.
	// 'Mac' options tailor the application when running an macOS.
	app := application.New(application.Options{
		Name:        "Beacon Scout",
		Description: "BLE beacon advertisement scanner (macOS / Windows)",
		Services: []application.Service{
			application.NewService(NewScanService(NewBeaconStore())),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	// Create a new window with the necessary options.
	// 'Title' is the title of the window.
	// 'Mac' options tailor the window when running on macOS.
	// 'BackgroundColour' is the background colour of the window.
	// 'URL' is the URL that will be loaded into the webview.
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "Beacon Scout",
		// Window sized to the golden ratio (1000 / 618 ≈ 1.618).
		Width:  1280,
		Height: 800,
		// Open the webview inspector automatically so binding errors and
		// scan logs are visible in the dev console without extra keystrokes.
		// In dev mode (`wails3 dev`) the inspector is available anyway via
		// Cmd+Option+I (macOS) / F12 (Windows/Linux).
		OpenInspectorOnStartup: true,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(6, 7, 15),
		URL:              "/",
	})

	// Run the application. This blocks until the application has been exited.
	err := app.Run()

	// If an error occurred while running the application, log it and exit.
	if err != nil {
		log.Fatal(err)
	}
}

// BeaconListEvent is the payload of the "beacons:list" event emitted by the
// ScanService. Registering it as an object lets the binding generator create a
// strongly typed TypeScript model for the frontend.
type BeaconListEvent struct {
	Scanning bool           `json:"scanning"`
	Total    int            `json:"total"`
	Items    []BeaconRecord `json:"items"`
}
