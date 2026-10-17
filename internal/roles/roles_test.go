package roles

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureProfileCopyUpgradesOnlyUneditedRetiredDefaults(t *testing.T) {
	dir := t.TempDir()
	leaderPath := filepath.Join(dir, "leader.json")
	retired := Role{SchemaVersion: 1, ID: LeaderRoleID, Name: "队长", Instructions: retiredLeaderText}
	if got := Digest(retired.Instructions); got != retiredDigests[LeaderRoleID][0] {
		t.Fatalf("retired leader digest = %s", got)
	}
	if err := writeRoleFile(leaderPath, retired); err != nil {
		t.Fatal(err)
	}
	edited := Role{SchemaVersion: 1, ID: "qa-engineer", Name: "测试", Instructions: "用户自己改过的指令"}
	qaPath := filepath.Join(dir, "qa_engineer.json")
	if err := writeRoleFile(qaPath, edited); err != nil {
		t.Fatal(err)
	}

	if err := EnsureProfileCopy(dir); err != nil {
		t.Fatal(err)
	}
	profile, err := LoadProfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	current, err := Default(LeaderRoleID)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range profile {
		switch role.ID {
		case LeaderRoleID:
			if role.Instructions != current.Instructions {
				t.Fatal("unedited retired leader copy was not upgraded")
			}
		case "qa-engineer":
			if role.Instructions != edited.Instructions {
				t.Fatal("user-edited role was overwritten")
			}
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "frontend_dev.json")); err != nil {
		t.Fatal("missing default role was not added")
	}
}

const retiredLeaderText = "你是这个团队的队长，负责把用户的需求拆成有唯一负责人的任务、协调成员之间的接口对齐、汇总证据并向用户汇报。你不是五种专业角色之一，不代替成员实现细节。你可以向成员发送结构化消息、指派任务、提议交接（需对方接受）；需要引入新成员时必须先请求用户批准。任务完成必须有相符证据，聊天里说\"完成\"不算完成。与用户对话时使用用户的语言，简洁、给出可核对的事实与待决事项。"
