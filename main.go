package main

import (
	"embed"
	"log"
	"os"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/icons"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.
// See https://pkg.go.dev/embed for more information.

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	application.RegisterEvent[JobView]("job:updated")
	application.RegisterEvent[RegistryView]("registry:updated")
	application.RegisterEvent[ServerView]("server:updated")
	application.RegisterEvent[ProjectView]("project:updated")
	application.RegisterEvent[DeploymentView]("deployment:updated")
}

func main() {
	if shouldRunMCP(os.Args[1:]) {
		os.Exit(runMCPCommand(os.Stderr))
	}
	if shouldRunCLI(os.Args[1:]) {
		os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
	}
	runDesktop()
}

func runDesktop() {
	systemService := NewSystemService()
	app := application.New(application.Options{
		Name:        "iDeploy",
		Description: "Local Docker delivery and remote operations for macOS",
		Services: []application.Service{
			application.NewService(systemService),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})
	systemService.setEmitter(func(value JobView) {
		app.Event.Emit("job:updated", value)
	})
	systemService.setRegistryEmitter(func(value RegistryView) {
		app.Event.Emit("registry:updated", value)
	})
	systemService.setServerEmitter(func(value ServerView) {
		app.Event.Emit("server:updated", value)
	})
	systemService.setProjectEmitter(func(value ProjectView) {
		app.Event.Emit("project:updated", value)
	})
	systemService.setDeploymentEmitter(func(value DeploymentView) {
		app.Event.Emit("deployment:updated", value)
	})

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "iDeploy",
		Name:      "main",
		Width:     1280,
		Height:    820,
		MinWidth:  1040,
		MinHeight: 680,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 52,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(244, 246, 248),
		URL:              "/",
	})

	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		window.Hide()
		event.Cancel()
	})
	app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(_ *application.ApplicationEvent) {
		window.Show()
	})
	app.Event.OnApplicationEvent(events.Mac.ApplicationDidFinishLaunching, func(_ *application.ApplicationEvent) {
		startAppUpdater()
	})

	tray := app.SystemTray.New()
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(icons.SystrayMacTemplate)
	}
	trayMenu := app.NewMenu()
	trayMenu.Add("打开 iDeploy").OnClick(func(_ *application.Context) { window.Show() })
	trayMenu.AddSeparator()
	trayMenu.Add("退出").OnClick(func(_ *application.Context) { app.Quit() })
	tray.SetMenu(trayMenu)
	tray.OnClick(func() { window.Show() })

	err := app.Run()
	if err != nil {
		log.Fatal(err)
	}
}
