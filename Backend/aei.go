package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const defaultVivadoPart = "xc7a35tfgg484-2"

var (
	safeNamePattern      = regexp.MustCompile(`[^A-Za-z0-9_-]`)
	hdlIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]*$`)
	safePathPattern      = regexp.MustCompile(`^[A-Za-z0-9_./\\-]+$`)
)

type AEIAction struct {
	Type        string `json:"type"`
	Action      string `json:"action"`
	ProjectName string `json:"projectName"`
	Part        string `json:"part"`
	Fileset     string `json:"fileset"`
	Top         string `json:"top"`
	SimTimeUS   int    `json:"simTimeUs"`
	XDCPath     string `json:"xdcPath"`
}

type AEICommand struct {
	Action  string
	Summary string
	Tcl     string
}

func ParseAEICommand(message []byte) (*AEICommand, bool, error) {
	var action AEIAction
	if err := json.Unmarshal(message, &action); err != nil {
		return nil, false, nil
	}
	if action.Type != "AEI" {
		return nil, false, nil
	}
	command, err := BuildAEICommand(action)
	if err != nil {
		return nil, true, err
	}
	return command, true, nil
}

func BuildAEICommand(action AEIAction) (*AEICommand, error) {
	switch action.Action {
	case "create_project":
		return buildCreateProjectCommand(action)
	case "set_top":
		return buildSetTopCommand(action)
	case "simulate":
		return buildSimulationCommand(action)
	case "import_constraint":
		return buildImportConstraintCommand(action)
	case "synth":
		return buildBuildStepCommand(action, "synth", []string{
			"update_compile_order -fileset sources_1",
			"reset_run synth_1",
			"launch_runs synth_1",
			"wait_on_run synth_1",
		})
	case "impl":
		return buildBuildStepCommand(action, "impl", []string{
			"update_compile_order -fileset sources_1",
			"reset_run impl_1",
			"launch_runs impl_1",
			"wait_on_run impl_1",
		})
	case "bitstream":
		return buildBuildStepCommand(action, "bitstream", []string{
			"open_run impl_1",
			"write_bitstream -force [get_property DIRECTORY [current_run]]/[get_property TOP [current_fileset]].bit",
		})
	default:
		return nil, fmt.Errorf("unknown AEI action: %s", action.Action)
	}
}

func buildCreateProjectCommand(action AEIAction) (*AEICommand, error) {
	projectName := safeProjectName(action.ProjectName)
	part := strings.TrimSpace(action.Part)
	if part == "" {
		part = defaultVivadoPart
	}
	if !safePathPattern.MatchString(part) {
		return nil, fmt.Errorf("invalid part name: %s", action.Part)
	}
	lines := []string{
		fmt.Sprintf(`puts "INFO: AEI action create_project project=%s part=%s"`, projectName, part),
		"if {[llength [get_projects -quiet]] != 0} {",
		`    puts "INFO: project already open."`,
		"} else {",
		"    set project_opened 0",
		fmt.Sprintf(`    set project_candidates [list "./%s/%s.xpr" "./%s.xpr" "../%s/%s.xpr" "../%s.xpr"]`, projectName, projectName, projectName, projectName, projectName, projectName),
		"    foreach project_xpr $project_candidates {",
		"        if {[file exists $project_xpr]} {",
		"            open_project $project_xpr",
		"            set project_opened 1",
		"            break",
		"        }",
		"    }",
		"    if {!$project_opened} {",
		fmt.Sprintf("        create_project %s ./%s -part %s", projectName, projectName, part),
		"    }",
		"}",
	}
	return &AEICommand{
		Action:  action.Action,
		Summary: fmt.Sprintf("create/open project %s", projectName),
		Tcl:     strings.Join(lines, "\n"),
	}, nil
}

func buildSetTopCommand(action AEIAction) (*AEICommand, error) {
	projectName := safeProjectName(action.ProjectName)
	top, err := requireHDLIdentifier(action.Top, "top")
	if err != nil {
		return nil, err
	}
	fileset := strings.TrimSpace(action.Fileset)
	if fileset == "" {
		fileset = "sources_1"
	}
	if fileset != "sources_1" && fileset != "sim_1" {
		return nil, fmt.Errorf("unsupported fileset: %s", fileset)
	}
	body := []string{
		fmt.Sprintf(`puts "INFO: AEI action set_top top=%s fileset=%s"`, top, fileset),
		fmt.Sprintf(`set top_name "%s"`, top),
		fmt.Sprintf(`set target_fileset "%s"`, fileset),
		"if {[llength [get_filesets $target_fileset]] == 0} {",
		`    puts "ERROR: fileset not found: $target_fileset"`,
		"} else {",
		"    set_property top $top_name [get_filesets $target_fileset]",
		"    set applied_top [get_property top [get_filesets $target_fileset]]",
		`    puts "INFO: top set for $target_fileset => $applied_top"`,
		"}",
	}
	return actionWithProject(action.Action, fmt.Sprintf("set %s top to %s", fileset, top), projectName, body), nil
}

func buildSimulationCommand(action AEIAction) (*AEICommand, error) {
	projectName := safeProjectName(action.ProjectName)
	simTime := action.SimTimeUS
	if simTime <= 0 {
		simTime = 100
	}
	if simTime > 1000000 {
		return nil, fmt.Errorf("simTimeUs is too large: %d", simTime)
	}
	body := []string{
		fmt.Sprintf(`puts "INFO: AEI action simulate time_us=%d"`, simTime),
		"set design_files [list]",
		"set tb_files [list]",
		"foreach f [glob -nocomplain ./*.v ./*.sv ./*.vhd ./*.vhdl] {",
		"    set fname [file tail $f]",
		`    if {[string match -nocase "tb*.v" $fname] || [string match -nocase "tb*.sv" $fname] || [string match -nocase "tb*.vhd" $fname] || [string match -nocase "tb*.vhdl" $fname] || [string match -nocase "*_tb.v" $fname] || [string match -nocase "*_tb.sv" $fname] || [string match -nocase "*_tb.vhd" $fname] || [string match -nocase "*_tb.vhdl" $fname]} {`,
		"        lappend tb_files $f",
		"    } else {",
		"        lappend design_files $f",
		"    }",
		"}",
		"if {[llength $design_files] != 0} {",
		"    catch {add_files -fileset sources_1 -norecurse $design_files}",
		"}",
		"if {[llength $tb_files] != 0} {",
		"    catch {add_files -fileset sim_1 -norecurse $tb_files}",
		"}",
		"update_compile_order -fileset sources_1",
		"update_compile_order -fileset sim_1",
		"set sim_top [string trim [get_property top [get_filesets sim_1]]]",
		"if {$sim_top eq \"\" && [llength $tb_files] != 0} {",
		"    set tb_guess [file rootname [file tail [lindex $tb_files 0]]]",
		"    if {$tb_guess ne \"\"} {",
		"        catch {set_property top $tb_guess [get_filesets sim_1]}",
		"        set sim_top [string trim [get_property top [get_filesets sim_1]]]",
		"    }",
		"}",
		"if {$sim_top eq \"\"} {",
		`    puts "ERROR: sim_1 top is empty. Please set Testbench top first."`,
		"} else {",
		"    if {[catch {launch_simulation} sim_err]} {",
		`        puts "ERROR: launch_simulation failed: $sim_err"`,
		"    } else {",
		"        open_vcd",
		"        restart",
		"        log_vcd",
		fmt.Sprintf("        run %dus", simTime),
		"        close_vcd",
		"        close_sim",
		"    }",
		"}",
	}
	return actionWithProject(action.Action, fmt.Sprintf("simulate for %dus", simTime), projectName, body), nil
}

func buildImportConstraintCommand(action AEIAction) (*AEICommand, error) {
	projectName := safeProjectName(action.ProjectName)
	xdcPath := strings.TrimSpace(action.XDCPath)
	if xdcPath == "" {
		return nil, errors.New("xdcPath is required")
	}
	if strings.Contains(xdcPath, "..") || strings.HasPrefix(xdcPath, "/") || strings.HasPrefix(xdcPath, "\\") || !safePathPattern.MatchString(xdcPath) {
		return nil, fmt.Errorf("invalid xdcPath: %s", action.XDCPath)
	}
	body := []string{
		fmt.Sprintf(`puts "INFO: AEI action import_constraint path=%s"`, xdcPath),
		fmt.Sprintf(`set xdc_path "%s"`, xdcPath),
		"if {[file exists $xdc_path]} {",
		"    catch {remove_files [get_files $xdc_path]}",
		"    catch {add_files -fileset constrs_1 -norecurse $xdc_path}",
		"    update_compile_order -fileset constrs_1",
		`    puts "INFO: constraint imported: $xdc_path"`,
		"} else {",
		`    puts "ERROR: xdc file not found: $xdc_path"`,
		"}",
	}
	return actionWithProject(action.Action, fmt.Sprintf("import constraint %s", xdcPath), projectName, body), nil
}

func buildBuildStepCommand(action AEIAction, name string, body []string) (*AEICommand, error) {
	projectName := safeProjectName(action.ProjectName)
	lines := append([]string{fmt.Sprintf(`puts "INFO: AEI action %s"`, name)}, body...)
	return actionWithProject(action.Action, name, projectName, lines), nil
}

func actionWithProject(action string, summary string, projectName string, bodyLines []string) *AEICommand {
	return &AEICommand{
		Action:  action,
		Summary: summary,
		Tcl:     buildEnsureProjectOpenTcl(projectName, bodyLines),
	}
}

func buildEnsureProjectOpenTcl(projectName string, bodyLines []string) string {
	projectName = safeProjectName(projectName)
	ensureOpenLines := []string{
		"set project_ready 0",
		"set cwd_xprs [glob -nocomplain ./*.xpr]",
		"if {[llength $cwd_xprs] != 0} {",
		"    set target_xpr [lindex $cwd_xprs 0]",
		"    if {[llength [get_projects -quiet]] != 0} {",
		"        catch {close_project}",
		"    }",
		"    if {[catch {open_project $target_xpr} open_err]} {",
		`        puts "ERROR: failed to open project in current folder: $open_err"`,
		"    } else {",
		"        set project_ready 1",
		"    }",
		"}",
		"if {!$project_ready} {",
		"    if {[llength [get_projects -quiet]] != 0} {",
		fmt.Sprintf("        if {[catch {save_project_as %s ./%s -force} save_err]} {", projectName, projectName),
		`            puts "ERROR: current project is in-memory and save_project_as failed: $save_err"`,
		"        } else {",
		"            set project_ready 1",
		"        }",
		"    }",
		"}",
		"if {!$project_ready} {",
		fmt.Sprintf(`    set project_candidates [list "./%s/%s.xpr" "./%s.xpr" "../%s/%s.xpr" "../%s.xpr" "../../%s/%s.xpr" "../../%s.xpr"]`, projectName, projectName, projectName, projectName, projectName, projectName, projectName, projectName, projectName),
		"    foreach project_xpr $project_candidates {",
		"        if {$project_ready} {",
		"            break",
		"        }",
		"        if {[file exists $project_xpr]} {",
		"            if {[catch {open_project $project_xpr} open_err]} {",
		`                puts "ERROR: failed to open project: $open_err"`,
		"            } else {",
		"                set project_ready 1",
		"                break",
		"            }",
		"        }",
		"    }",
		"}",
		"if {!$project_ready} {",
		`    puts "ERROR: no opened project and no .xpr found for this project name."`,
		"}",
		"if {$project_ready} {",
	}
	lines := make([]string, 0, len(ensureOpenLines)+len(bodyLines)+1)
	lines = append(lines, ensureOpenLines...)
	for _, line := range bodyLines {
		lines = append(lines, "    "+line)
	}
	lines = append(lines, "}")
	return strings.Join(lines, "\n")
}

func safeProjectName(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		name = "my_project"
	}
	name = safeNamePattern.ReplaceAllString(name, "_")
	if name == "" {
		return "my_project"
	}
	return name
}

func requireHDLIdentifier(raw string, field string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if !hdlIdentifierPattern.MatchString(value) {
		return "", fmt.Errorf("invalid %s: %s", field, raw)
	}
	return value, nil
}

func tclPutsError(message string) string {
	escaped := strings.ReplaceAll(message, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return fmt.Sprintf(`puts "ERROR: %s"`, escaped)
}
