package store

import (
	"os"
	"path/filepath"
	"testing"
)

// 自建渠道的探测结论必须跨进程存活：重启后 probes.json 要能还原，
// 否则界面把已经测通的模型退回「待测」，用户看到的就是"我加的模型不见了"。
func TestProbedVerdictsSurviveReopen(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZEN_GATE_HOME", home)

	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	s.SetProbedVerdict("qoder/DeepSeek-Flash", ProbedVerdict{
		State: "available", TTFTMs: 8, At: 1794119465147,
	})

	if _, err := os.Stat(filepath.Join(home, "probes.json")); err != nil {
		t.Fatalf("probes.json 没写出来: %v", err)
	}

	again, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	got := again.SnapshotProbedVerdicts()
	v, ok := got["qoder/DeepSeek-Flash"]
	if !ok {
		t.Fatalf("重开后没有还原探测结论: %+v", got)
	}
	if v.State != "available" || v.TTFTMs != 8 || v.At != 1794119465147 {
		t.Errorf("还原出来的值不对: %+v", v)
	}
}

// 空目录不该凭空造出结论：一张没有 probes.json 的数据目录必须读出零条。
func TestProbedVerdictsEmptyByDefault(t *testing.T) {
	t.Setenv("ZEN_GATE_HOME", t.TempDir())
	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if got := s.SnapshotProbedVerdicts(); len(got) != 0 {
		t.Errorf("空目录却读出了 %d 条: %+v", len(got), got)
	}
}

// 后一次探测覆盖前一次：同一个模型测两遍只留最后那个结论。
func TestSetProbedVerdictOverwrites(t *testing.T) {
	t.Setenv("ZEN_GATE_HOME", t.TempDir())
	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	s.SetProbedVerdict("m", ProbedVerdict{State: "unavailable", At: 1})
	s.SetProbedVerdict("m", ProbedVerdict{State: "available", TTFTMs: 20, At: 2})
	v := s.SnapshotProbedVerdicts()["m"]
	if v.State != "available" || v.TTFTMs != 20 {
		t.Errorf("后一次没覆盖前一次: %+v", v)
	}
}
