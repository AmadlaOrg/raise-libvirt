package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/AmadlaOrg/raise-libvirt/libvirt"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	appName = "raise-libvirt"
	version = "1.0.0"
)

var rootCmd = &cobra.Command{
	Use:     appName,
	Short:   "Raise plugin for managing KVM/QEMU VMs via libvirt",
	Version: version,
}

var (
	infoOutputFlag string
	infoHeryFlag   bool

	infoCmd = &cobra.Command{
		Use:   "info",
		Short: "Show plugin metadata",
		Run: func(cmd *cobra.Command, args []string) {
			metadata := map[string]any{
				"name":    appName,
				"version": version,
				"engine":  "libvirt",
				"supports": []string{
					"amadla.org/entity/infrastructure@^v1.0.0",
					"amadla.org/entity/infrastructure/vm@^v1.0.0",
				},
				"description": "Manages KVM/QEMU virtual machines via libvirt",
			}
			if err := writeInfoOutput(os.Stdout, infoOutputFlag, infoHeryFlag, metadata); err != nil {
				fmt.Fprintf(os.Stderr, "Error encoding metadata: %v\n", err)
				os.Exit(1)
			}
		},
	}
)

var (
	upFilePath string

	upCmd = &cobra.Command{
		Use:   "up [name]",
		Short: "Create and start a VM",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runUp,
	}
)

var haltCmd = &cobra.Command{
	Use:   "halt <name>",
	Short: "Shut down a VM",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := libvirt.New()
		if err := mgr.Halt(args[0]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "VM %q is shutting down\n", args[0])
		return nil
	},
}

var destroyCmd = &cobra.Command{
	Use:   "destroy <name>",
	Short: "Destroy a VM and remove all storage",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := libvirt.New()
		if err := mgr.Destroy(args[0]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "VM %q has been destroyed\n", args[0])
		return nil
	},
}

var (
	sshUser    string
	sshPort    int
	sshKeyFile string

	sshCmd = &cobra.Command{
		Use:   "ssh <name>",
		Short: "SSH into a VM",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := libvirt.New()
			if err := mgr.SSH(args[0], sshUser, sshPort, sshKeyFile); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return nil
		},
	}
)

var statusCmd = &cobra.Command{
	Use:   "status [name]",
	Short: "Show VM status",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := libvirt.New()

		if len(args) == 1 {
			state, err := mgr.Status(args[0])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return libvirt.OutputJSON(state)
		}

		states, err := mgr.StatusAll()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return libvirt.OutputJSON(states)
	},
}

func init() {
	infoCmd.Flags().StringVarP(&infoOutputFlag, "output", "o", "table", "Output format: table, json, yaml")
	infoCmd.Flags().BoolVar(&infoHeryFlag, "hery", false, "Wrap output in HERY envelope (_type, _body)")

	upCmd.Flags().StringVarP(&upFilePath, "file", "f", "", "Input data file (JSON or YAML; use '-' for stdin)")
	_ = upCmd.MarkFlagRequired("file")

	sshCmd.Flags().StringVarP(&sshUser, "user", "u", "", "SSH user (default: from entity or root)")
	sshCmd.Flags().IntVarP(&sshPort, "port", "p", 22, "SSH port")
	sshCmd.Flags().StringVarP(&sshKeyFile, "key", "k", "", "SSH private key file")

	rootCmd.AddCommand(infoCmd)
	rootCmd.AddCommand(upCmd)
	rootCmd.AddCommand(haltCmd)
	rootCmd.AddCommand(destroyCmd)
	rootCmd.AddCommand(sshCmd)
	rootCmd.AddCommand(statusCmd)
}

func writeInfoOutput(w io.Writer, format string, hery bool, data map[string]any) error {
	if hery {
		return writeHeryOutput(w, format, data)
	}

	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	case "yaml":
		bytes, err := yaml.Marshal(data)
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(w, string(bytes))
		return err
	default:
		table := tablewriter.NewWriter(w)
		table.Header("Field", "Value")
		table.Append("Name", fmt.Sprint(data["name"]))
		table.Append("Version", fmt.Sprint(data["version"]))
		table.Append("Engine", fmt.Sprint(data["engine"]))
		table.Append("Description", fmt.Sprint(data["description"]))
		if supports, ok := data["supports"].([]string); ok {
			table.Append("Supports", strings.Join(supports, "\n"))
		}
		table.Render()
		return nil
	}
}

type heryEnvelope struct {
	Type string `json:"_type" yaml:"_type"`
	Body any    `json:"_body" yaml:"_body"`
}

func writeHeryOutput(w io.Writer, format string, data map[string]any) error {
	envelope := heryEnvelope{
		Type: "amadla.org/entity/tools/info@v1.0.0",
		Body: data,
	}

	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(envelope)
	case "table":
		fmt.Fprintf(w, "_type: %s\n\n", envelope.Type)
		table := tablewriter.NewWriter(w)
		table.Header("Field", "Value")
		table.Append("Name", fmt.Sprint(data["name"]))
		table.Append("Version", fmt.Sprint(data["version"]))
		table.Append("Engine", fmt.Sprint(data["engine"]))
		table.Append("Description", fmt.Sprint(data["description"]))
		if supports, ok := data["supports"].([]string); ok {
			table.Append("Supports", strings.Join(supports, "\n"))
		}
		table.Render()
		return nil
	default:
		bytes, err := yaml.Marshal(envelope)
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(w, string(bytes))
		return err
	}
}

func runUp(cmd *cobra.Command, args []string) error {
	var input io.Reader

	if upFilePath == "-" {
		input = os.Stdin
	} else {
		f, err := os.Open(upFilePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot open file: %v\n", err)
			os.Exit(2)
		}
		defer f.Close()
		input = f
	}

	data, err := io.ReadAll(input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot read input: %v\n", err)
		os.Exit(2)
	}

	config, err := parseEntityToConfig(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot parse input: %v\n", err)
		os.Exit(2)
	}

	// Override name from positional argument if provided
	if len(args) == 1 {
		config.Name = args[0]
	}

	mgr := libvirt.New()
	state, err := mgr.Up(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	return libvirt.OutputJSON(state)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(2)
	}
}
