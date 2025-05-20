package command

import (
	"os"
	"slices"
	"strings"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/renderer"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/urfave/cli/v2"

	"github.com/opencloud-eu/opencloud/pkg/config/configlog"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/config"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/config/parser"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/unifiedrole"
)

var (
	unifiedRolesNames = map[string]string{
		unifiedrole.UnifiedRoleViewerID:                     "Viewer",
		unifiedrole.UnifiedRoleViewerListGrantsID:           "ViewerListGrants",
		unifiedrole.UnifiedRoleSpaceViewerID:                "SpaceViewer",
		unifiedrole.UnifiedRoleEditorID:                     "Editor",
		unifiedrole.UnifiedRoleEditorListGrantsID:           "EditorListGrants",
		unifiedrole.UnifiedRoleSpaceEditorID:                "SpaceEditor",
		unifiedrole.UnifiedRoleSpaceEditorWithoutVersionsID: "SpaceEditorWithoutVersions",
		unifiedrole.UnifiedRoleFileEditorID:                 "FileEditor",
		unifiedrole.UnifiedRoleFileEditorListGrantsID:       "FileEditorListGrants",
		unifiedrole.UnifiedRoleEditorLiteID:                 "EditorLite",
		unifiedrole.UnifiedRoleManagerID:                    "SpaceManager",
		unifiedrole.UnifiedRoleSecureViewerID:               "SecureViewer",
	}
)

// UnifiedRoles bundles available commands for unified roles
func UnifiedRoles(cfg *config.Config) cli.Commands {
	cmds := cli.Commands{
		listUnifiedRoles(cfg),
	}

	for _, cmd := range cmds {
		cmd.Category = "unified-roles"
		cmd.Name = strings.Join([]string{cmd.Name, "unified-roles"}, "-")
		cmd.Before = func(c *cli.Context) error {
			return configlog.ReturnError(parser.ParseConfig(cfg))
		}
	}

	return cmds
}

// unifiedRolesStatus lists available unified roles, it contains an indicator to show if the role is enabled or not
func listUnifiedRoles(cfg *config.Config) *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "list available unified roles",
		Action: func(c *cli.Context) error {
			r := tw.Rendition{
				Settings: tw.Settings{
					Separators: tw.Separators{
						BetweenRows: tw.On,
					},
				},
			}
			tbl := tablewriter.NewTable(os.Stdout, tablewriter.WithRenderer(renderer.NewBlueprint(r)))

			headers := []string{"Name", "UID", "Enabled", "Description", "Condition", "Allowed resource actions"}
			tbl.Header(headers)

			for _, definition := range unifiedrole.GetRoles(unifiedrole.RoleFilterAll()) {
				const enabled = "enabled"
				const disabled = "disabled"

				rows := [][]string{
					{unifiedRolesNames[definition.GetId()], definition.GetId(), disabled, definition.GetDescription()},
				}
				if slices.Contains(cfg.UnifiedRoles.AvailableRoles, definition.GetId()) {
					rows[0][2] = enabled
				}

				for i, rolePermission := range definition.GetRolePermissions() {
					actions := strings.Join(rolePermission.GetAllowedResourceActions(), "\n")
					row := []string{rolePermission.GetCondition(), actions}
					switch i {
					case 0:
						rows[0] = append(rows[0], row...)
					default:
						rows[0][4] = rows[0][4] + "\n" + rolePermission.GetCondition()
					}
				}

				for _, row := range rows {
					// balance the row before adding it to the table,
					// this prevents the row from having empty columns.
					tbl.Append(append(row, make([]string, len(headers)-len(row))...))
				}
			}

			tbl.Render()
			return nil
		},
	}
}
