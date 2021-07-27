package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/posener/complete"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hashicorp/waypoint-plugin-sdk/terminal"
	"github.com/hashicorp/waypoint/internal/clierrors"
	"github.com/hashicorp/waypoint/internal/pkg/flag"
	pb "github.com/hashicorp/waypoint/internal/server/gen"
)

type StatusCommand struct {
	*baseCommand

	flagContextName string
	flagVerbose     bool
	flagJson        bool
	flagAllProjects bool
	filterFlags     filterFlags
}

func (c *StatusCommand) Run(args []string) int {
	flagSet := c.Flags()
	// Initialize. If we fail, we just exit since Init handles the UI.
	// TODO: this doesn't support running waypoint commands outside of a project dir
	if err := c.Init(
		WithArgs(args),
		WithFlags(flagSet),
		WithSingleApp(),
	); err != nil {
		return 1
	}

	var ctxName string
	defaultName, err := c.contextStorage.Default()
	if err != nil {
		c.ui.Output(
			"Error getting default context: %s",
			clierrors.Humanize(err),
			terminal.WithErrorStyle(),
		)
		return 1
	}
	ctxName = defaultName

	ctxConfig, err := c.contextStorage.Load(ctxName)
	if err != nil {
		c.ui.Output("Error loading context %q: %s", ctxName, err.Error(), terminal.WithErrorStyle())
		return 1
	}

	cmdArgs := flagSet.Args()

	if len(cmdArgs) > 1 {
		c.ui.Output("No more than 1 argument required.\n\n"+c.Help(), terminal.WithErrorStyle())
		return 1
	}

	// Use-Cases for data ouptut
	// Project scoped info
	// 1. waypoint status
	// 2. waypoint status project
	// 2.1 waypoint status (inside a project dir)
	// Application scoped info
	// 3 waypoint status project/app
	// 3.1 waypoint status project -app=app

	var projectTarget, appTarget string
	if len(cmdArgs) >= 1 {
		s := cmdArgs[0]
		target := strings.Split(s, "/") // This breaks if we allow projects with "/" in the name

		projectTarget = target[0]
		if len(target) == 2 {
			appTarget = target[1]
		}

	} else if len(cmdArgs) == 0 {
		// We're in a project dir
		projectTarget = c.project.Ref().Project
	}

	if appTarget == "" && c.flagApp != "" {
		appTarget = c.flagApp
	} else if appTarget != "" && c.flagApp != "" {
		c.ui.Output(wpAppFlagAndTargetIncludedMsg, terminal.WithWarningStyle())
	}

	if projectTarget == "" || c.flagAllProjects {
		// Show high-level status of all projects
		c.ui.Output(wpStatusMsg, ctxConfig.Server.Address)

		err = c.FormatProjectStatus()
		if err != nil {
			c.ui.Output("Failed to format project statuses", terminal.WithErrorStyle())
			c.ui.Output(clierrors.Humanize(err), terminal.WithErrorStyle())
			return 1
		}
	} else if projectTarget != "" && appTarget == "" {
		// Show status of apps inside project
		c.ui.Output(wpStatusProjectMsg, projectTarget, ctxConfig.Server.Address)
	} else if projectTarget != "" && appTarget != "" {
		// Advanced view of a single app status
		c.ui.Output(wpStatusAppProjectMsg, appTarget, projectTarget, ctxConfig.Server.Address)
	}

	return 0
}

func (c *StatusCommand) FormatProjectStatus() error {
	// Get our API client
	client := c.project.Client()

	projectResp, err := client.ListProjects(c.Ctx, &empty.Empty{})
	if err != nil {
		c.ui.Output("Failed to retrieve all projects", terminal.WithErrorStyle())
		c.ui.Output(clierrors.Humanize(err), terminal.WithErrorStyle())
		return err
	}
	projNameList := projectResp.Projects

	headers := []string{
		"Project", "Workspace", "App Statuses",
	}

	tbl := terminal.NewTable(headers...)

	for _, projectRef := range projNameList {
		resp, err := client.GetProject(c.Ctx, &pb.GetProjectRequest{
			Project: projectRef,
		})
		if err != nil {
			return err
		}

		var workspace string
		if len(resp.Workspaces) == 0 {
			// this happens if you just wapyoint init
			// probably a bug?
			workspace = "???"
		} else {
			workspace = resp.Workspaces[0].Workspace.Workspace // TODO: assume the first workspace is correct??
		}

		// Get App Statuses
		var appStatusReports []*pb.StatusReport
		for _, app := range resp.Project.Applications {
			if workspace == "???" {
				workspace = "default"
			}
			appStatusResp, err := client.GetLatestStatusReport(c.Ctx, &pb.GetLatestStatusReportRequest{
				Application: &pb.Ref_Application{
					Application: app.Name,
					Project:     resp.Project.Name,
				},
				Workspace: &pb.Ref_Workspace{
					Workspace: workspace,
				},
			})
			if status.Code(err) == codes.NotFound {
				// App doesn't have a status report yet, likely not deployed
				err = nil
				continue
			}
			if err != nil {
				return err
			}

			appStatusReports = append(appStatusReports, appStatusResp)
		}

		// TODO: generate aggregate health for all apps first
		statusReportComplete := "N/A"
		//var lastRelevantAppStatus *pb.StatusReport

		if len(appStatusReports) != 0 {
			switch appStatusReports[0].Health.HealthStatus {
			case "READY":
				statusReportComplete = "✔ READY"
			case "ALIVE":
				statusReportComplete = "✔ ALIVE"
			case "DOWN":
				statusReportComplete = "✖ DOWN"
			case "PARTIAL":
				statusReportComplete = "● PARTIAL"
			case "UNKNOWN":
				statusReportComplete = "? UNKNOWN"
			}

			if t, err := ptypes.Timestamp(appStatusReports[0].GeneratedTime); err == nil {
				statusReportComplete = fmt.Sprintf("%s - %s", statusReportComplete, humanize.Time(t))
			}
		}

		statusColor := ""
		columns := []string{
			resp.Project.Name,
			workspace,
			statusReportComplete, // app statuses overall
		}

		// Add column data to table
		tbl.Rich(
			columns,
			[]string{
				statusColor,
			},
		)
	}

	// Sort by Name, Workspace, or Status
	// might have to pre-sort by status since strings are ascii

	// Render the table
	c.ui.Output("")
	c.ui.Table(tbl, terminal.WithStyle("Simple"))
	c.ui.Output("")
	c.ui.Output(wpStatusSuccessMsg)

	return nil
}

func (c *StatusCommand) displayJson() error {
	var output []map[string]interface{}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}

	c.ui.Output(string(data))
	return nil
}

func (c *StatusCommand) GetProjects() ([]*pb.Project, error) {
	// Get our API client
	client := c.project.Client()

	projectResp, err := client.ListProjects(c.Ctx, &empty.Empty{})
	if err != nil {
		return nil, err
	}
	projNameList := projectResp.Projects

	var projects []*pb.Project
	for _, projRef := range projNameList {
		resp, err := client.GetProject(c.Ctx, &pb.GetProjectRequest{
			Project: projRef,
		})
		if err != nil {
			return nil, err
		}
		project := resp.Project
		// WHAT ABOUT WORKSPACES?

		projects = append(projects, project)
	}

	return projects, nil

}

func (c *StatusCommand) Flags() *flag.Sets {
	return c.flagSet(0, func(set *flag.Sets) {
		f := set.NewSet("Command Options")

		f.BoolVar(&flag.BoolVar{
			Name:    "verbose",
			Aliases: []string{"V"},
			Target:  &c.flagVerbose,
			Usage:   "Display more details.",
		})

		f.BoolVar(&flag.BoolVar{
			Name:   "json",
			Target: &c.flagJson,
			Usage:  "Output the status information as JSON.",
		})

		f.BoolVar(&flag.BoolVar{
			Name:   "all-projects",
			Target: &c.flagAllProjects,
			Usage:  "Output status about every project in a workspace.",
		})

		initFilterFlags(set, &c.filterFlags, fillterOptionAll)
	})
}

func (c *StatusCommand) AutocompleteArgs() complete.Predictor {
	return complete.PredictNothing
}

func (c *StatusCommand) AutocompleteFlags() complete.Flags {
	return c.Flags().Completions()
}

func (c *StatusCommand) Synopsis() string {
	return "List statuses."
}

func (c *StatusCommand) Help() string {
	return formatHelp(`
Usage: waypoint status [options] [project]

  View the current status of projects and applications managed by Waypoint.

` + c.Flags().Help())
}

var (
	// Success or info messages

	wpStatusSuccessMsg = strings.TrimSpace(`
The projects listed above represent their current state known
in the Waypoint server. For more information about a project’s applications and
their current state, run ‘waypoint status PROJECT-NAME’.
`)

	wpStatusMsg = "Current project statuses in server context %q"

	wpStatusProjectMsg = "Current status for project %q in server context %q."

	wpStatusAppProjectMsg = strings.TrimSpace(`
Current status for application % q in project %q in server context %q.
`)

	// Failure messages

	// TODO how to show hints for multiple app failures
	wpStatusHealthTriageMsg = strings.TrimSpace(`
To see more information about the failing application, please check out the application logs:

waypoint logs -app=%[1]s

The projects listed above represent their current state known
in Waypoint server. For more information about an application defined in the project %[1]q can be viewed by running the command:

waypoint status %[2]s -app=%[1]s.
`)

	wpProjectNotFound = strings.TrimeSpace(`
No project name %q was found for the server context %q. To see a list of
currently configured projects, run “waypoint project list”.

If you want more information for a specific application, use the '-app' flag
with “waypoint status PROJECT-NAME -app=APP-NAME”.
`)

	wpAppFlagAndTargetIncludedMsg = strings.TrimeSpace(`
The 'app' flag was included, but an application was also requested as an argument.
The app flag will be ignored.
`)

	// TODO do we need a "waypoint application list"
	wpAppNotFound = strings.TrimSpace(`
No app name %q was found in project %q for the server context %q. To see a
list of currently configured projects, run “waypoint project list”.
`)
)
