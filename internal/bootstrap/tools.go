package bootstrap

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kriuchkov/herdr-scope/internal/adapters/scenario"
	"github.com/kriuchkov/herdr-scope/internal/adapters/sidebar"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

func renderCommand() *cobra.Command {
	var input string
	var annotate, tokens bool
	var layout string
	cmd := &cobra.Command{
		Use:   "render",
		Short: "Replay a scenario and print the sidebar as Herdr would draw it (golden text)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := scenario.Load(input)
			if err != nil {
				return err //nolint:wrapcheck // names the file
			}
			s.Layout = ports.Layout(layout)
			result, err := scenario.Run(s)
			if err != nil {
				return err //nolint:wrapcheck // names the scenario
			}
			if tokens {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result) //nolint:wrapcheck // stdout
			}
			fmt.Fprint(cmd.OutOrStdout(), result.Render(sidebar.RenderOptions{Annotate: annotate}))
			return nil
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "scenario JSON")
	cmd.Flags().BoolVar(&annotate, "annotate", false, "name the token behind every visible cell")
	cmd.Flags().BoolVar(&tokens, "tokens", false, "print the final token store as JSON instead")
	cmd.Flags().StringVar(&layout, "layout", "", "compact or classic; default: the scenario config's")
	_ = cmd.MarkFlagRequired("input")
	return cmd
}
