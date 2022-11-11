package cmds

import (
	"encoding/json"
	"fmt"
	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
	"github.com/DataDog/datadog-api-client-go/v2/api/datadogV2"
	"github.com/araddon/dateparse"
	_ "github.com/araddon/dateparse"
	"github.com/scylladb/termtables"
	_ "github.com/scylladb/termtables"
	"github.com/spf13/cobra"
	"os"
	"strings"
)

var RumCmd = cobra.Command{
	Use:   "rum",
	Short: "Query DataDog RUM",
	Run: func(cmd *cobra.Command, args []string) {
	},
}

type Action struct {
	Name       string
	Attributes map[string]interface{}
	Context    interface{}
}

var listActionsCmd = cobra.Command{
	Use:   "ls-actions",
	Short: "List RUM actions",
	Long: `List RUM actions.

This command allows you to specify action names to look for, using comma-separated lists:

    dd-cli rum ls-actions --action "action1,action2"

The output can be JSON, in which case you get the entire action objects:

	dd-cli rum ls-actions --action action1 --count 23 --output json

When using the output types other than JSON, the data in the "context" field gets turned into 
individual columns. The other objects are ignored.

You can also specify fields to include in the output, using comma-separated lists:

	dd-cli rum ls-actions --fields "name,page.url"

You can also specify fields to remove from the output, using comma-separated lists:

	dd-cli rum ls-actions --filter "page.url"
	`,
	Run: func(cmd *cobra.Command, args []string) {
		from := cmd.Flag("from").Value.String()
		to := cmd.Flag("to").Value.String()
		output := cmd.Flag("output").Value.String()
		_ = cmd.Flag("output-file").Value.String()
		action := cmd.Flag("action").Value.String()
		actionNames := []string{}
		if action != "" {
			actionNames = strings.Split(action, ",")
		}
		fieldStr := cmd.Flag("fields").Value.String()
		filters := []string{}
		fields := []string{}
		if fieldStr != "" {
			fields = strings.Split(fieldStr, ",")
		}
		filterStr := cmd.Flag("filter").Value.String()
		if filterStr != "" {
			filters = strings.Split(filterStr, ",")
		}

		filter := datadogV2.NewRUMQueryFilter()
		query := "@type:action"
		if action != "" {
			query += fmt.Sprintf(" @action.name:(%s)", strings.Join(actionNames, " OR "))
		}

		filter.SetQuery(query)
		if from != "" {
			t, err := dateparse.ParseLocal(from)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Could not parse %s: %s", from, err)
				return
			}
			filter.SetFrom(fmt.Sprintf("%d", t.Unix()))
		}
		if to != "" {
			t, err := dateparse.ParseLocal(to)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Could not parse %s: %s", t, err)
				return
			}
			filter.SetTo(fmt.Sprintf("%d", t.Unix()))
		}

		body := datadogV2.NewRUMSearchEventsRequest()
		body.SetFilter(*filter)
		configuration := datadog.NewConfiguration()
		apiClient := datadog.NewAPIClient(configuration)
		rumApi := datadogV2.NewRUMApi(apiClient)

		events, cancel, err := rumApi.SearchRUMEventsWithPagination(CmdContext, *body)
		if err != nil {
			panic(err)
		}
		defer cancel()

		actions := []Action{}

		count, err := cmd.Flags().GetInt("count")
		if err != nil {
			panic(err)
		}

		for i := 0; i < count; i++ {
			e := <-events
			attrs := e.GetAttributes().Attributes
			action, hasAction := attrs["action"].(map[string]interface{})
			if !hasAction {
				continue
			}
			actions = append(actions, Action{
				Name:       action["name"].(string),
				Attributes: attrs,
				Context:    attrs["context"],
			})
		}

		if output == "json" {
			jsonBytes, err := json.MarshalIndent(actions, "", "  ")
			if err != nil {
				panic(err)
			}
			fmt.Println(string(jsonBytes))
		} else if output == "table" {
			table := termtables.CreateTable()

			flattenedActions := flattenActions(actions)

			columns := collectContextColumns(flattenedActions, fields, filters)
			columnsInterface := make([]interface{}, len(columns))
			for i, v := range columns {
				columnsInterface[i] = v
			}

			table.AddHeaders("name")
			table.AddHeaders(columnsInterface...)

			for _, action := range flattenedActions {
				var row []interface{}
				row = append(row, action["name"])
				for _, column := range columns {
					s := ""
					if v, ok := action[column]; ok {
						s = fmt.Sprintf("%v", v)
					}
					row = append(row, s)
				}
				table.AddRow(row...)
			}

			fmt.Println(table.Render())
		} else if output == "csv" {

		}
	},
}

func flattenMapIntoColumns(rows map[string]interface{}) map[string]interface{} {
	ret := map[string]interface{}{}

	for key, value := range rows {
		switch v := value.(type) {
		case map[string]interface{}:
			for k, v := range flattenMapIntoColumns(v) {
				ret[fmt.Sprintf("%s.%s", key, k)] = v
			}
		default:
			ret[key] = v
		}
	}

	return ret
}

func flattenActions(actions []Action) []map[string]interface{} {
	ret := []map[string]interface{}{}

	for _, action := range actions {
		row := map[string]interface{}{}
		row["name"] = action.Name
		if action.Context != nil {
			context := action.Context.(map[string]interface{})
			for k, v := range flattenMapIntoColumns(context) {
				row[k] = v
			}
		}
		ret = append(ret, row)
	}

	return ret
}

func collectContextColumns(rows []map[string]interface{}, fields []string, filters []string) []string {
	ret := map[string]interface{}{}

	for _, row := range rows {
	Keys:
		for key := range row {
			if key != "name" {
				if len(filters) > 0 {
					for _, filter := range filters {
						if strings.HasSuffix(filter, ".") {
							if strings.HasPrefix(key, filter) {
								continue Keys
							}
						} else {
							if key == filter {
								continue Keys
							}
						}
					}
				}

				if len(fields) > 0 {
					for _, field := range fields {
						if strings.HasSuffix(field, ".") {
							if strings.HasPrefix(key, field) {
								ret[key] = nil
							}
						} else {
							if key == field {
								ret[key] = nil
							}
						}
					}
				} else {
					ret[key] = nil
				}
			}
		}
	}

	var keys []string
	for k := range ret {
		keys = append(keys, k)
	}

	return keys
}

func init() {
	RumCmd.AddCommand(&listActionsCmd)

	listActionsCmd.Flags().String("from", "", "From date (accepts variety of formats)")
	listActionsCmd.Flags().String("to", "", "To date (accepts variety of formats)")

	listActionsCmd.Flags().StringP("output", "o", "table", "Output format (table, csv, json, sqlite)")
	listActionsCmd.Flags().StringP("output-file", "f", "", "Output file")
	listActionsCmd.Flags().StringP("action", "a", "", "Action name")
	listActionsCmd.Flags().String("fields", "", "Fields to include in the output, default: all")
	listActionsCmd.Flags().String("filter", "", "Fields to remove from output")

	listActionsCmd.Flags().IntP("count", "c", 20, "Number of results to return")
}