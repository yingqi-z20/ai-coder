package main

import (
	"strings"
	"testing"
)

func TestParseAEICommandBuildsAction(t *testing.T) {
	command, handled, err := ParseAEICommand([]byte(`{"type":"AEI","action":"set_top","projectName":"demo","top":"tb_counter","fileset":"sim_1"}`))
	if err != nil {
		t.Fatalf("ParseAEICommand returned error: %v", err)
	}
	if !handled {
		t.Fatal("expected AEI message to be handled")
	}
	if command == nil {
		t.Fatal("expected AEI command")
	}
	if command.Action != "set_top" {
		t.Fatalf("unexpected action: %s", command.Action)
	}
	if !strings.Contains(command.Tcl, "set_property top $top_name [get_filesets $target_fileset]") {
		t.Fatalf("expected set_top Tcl, got:\n%s", command.Tcl)
	}
}

func TestParseAEICommandIgnoresRawTcl(t *testing.T) {
	command, handled, err := ParseAEICommand([]byte("launch_runs synth_1\n"))
	if err != nil {
		t.Fatalf("ParseAEICommand returned error: %v", err)
	}
	if handled {
		t.Fatalf("raw Tcl should not be handled as AEI: %#v", command)
	}
}

func TestBuildAEICommandRejectsInvalidTop(t *testing.T) {
	_, err := BuildAEICommand(AEIAction{
		Type:        "AEI",
		Action:      "set_top",
		ProjectName: "demo",
		Fileset:     "sources_1",
		Top:         "bad;top",
	})
	if err == nil {
		t.Fatal("expected invalid top to be rejected")
	}
}

func TestBuildAEICommandSanitizesProjectName(t *testing.T) {
	command, err := BuildAEICommand(AEIAction{
		Type:        "AEI",
		Action:      "create_project",
		ProjectName: "demo;rm",
	})
	if err != nil {
		t.Fatalf("BuildAEICommand returned error: %v", err)
	}
	if !strings.Contains(command.Tcl, "create_project demo_rm ./demo_rm") {
		t.Fatalf("expected sanitized project name, got:\n%s", command.Tcl)
	}
}
