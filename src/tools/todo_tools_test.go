package tools

import (
	"context"
	"testing"
)

// --- TodoManager ---

func TestTodoManager_Create(t *testing.T) {
	tm := NewTodoManager()

	item := tm.Create("测试任务", "")
	if item.ID != "1" {
		t.Errorf("期望 ID '1', 得到 '%s'", item.ID)
	}
	if item.Subject != "测试任务" {
		t.Errorf("期望 Subject '测试任务', 得到 '%s'", item.Subject)
	}
	if item.Status != TodoPending {
		t.Errorf("期望 Status 'pending', 得到 '%s'", item.Status)
	}

	// 创建第二个
	item2 := tm.Create("任务二", TodoInProgress)
	if item2.ID != "2" {
		t.Errorf("期望 ID '2', 得到 '%s'", item2.ID)
	}
	if item2.Status != TodoInProgress {
		t.Errorf("期望 Status 'in_progress', 得到 '%s'", item2.Status)
	}
}

func TestTodoManager_Update(t *testing.T) {
	tm := NewTodoManager()
	tm.Create("任务一", "")

	// 更新状态
	item := tm.Update("1", TodoInProgress, "")
	if item == nil {
		t.Fatal("更新失败")
	}
	if item.Status != TodoInProgress {
		t.Errorf("期望 Status 'in_progress', 得到 '%s'", item.Status)
	}
	if item.Subject != "任务一" {
		t.Errorf("Subject 不应改变, 得到 '%s'", item.Subject)
	}

	// 更新标题
	item = tm.Update("1", "", "新标题")
	if item.Subject != "新标题" {
		t.Errorf("期望 Subject '新标题', 得到 '%s'", item.Subject)
	}

	// 同时更新
	item = tm.Update("1", TodoCompleted, "最终标题")
	if item.Status != TodoCompleted || item.Subject != "最终标题" {
		t.Errorf("更新失败: [%s] %s", item.Status, item.Subject)
	}

	// 不存在的 ID
	item = tm.Update("999", TodoPending, "")
	if item != nil {
		t.Error("不存在的 ID 应返回 nil")
	}
}

func TestTodoManager_Get(t *testing.T) {
	tm := NewTodoManager()
	tm.Create("任务一", "")
	tm.Create("任务二", "")

	item := tm.Get("2")
	if item == nil || item.Subject != "任务二" {
		t.Error("Get #2 失败")
	}

	item = tm.Get("999")
	if item != nil {
		t.Error("不存在的 ID 应返回 nil")
	}
}

func TestTodoManager_GetItems(t *testing.T) {
	tm := NewTodoManager()
	tm.Create("A", "")
	tm.Create("B", "")

	items := tm.GetItems()
	if len(items) != 2 {
		t.Errorf("期望 2 项, 得到 %d", len(items))
	}

	// 浅拷贝：修改指针内容会影响原始数据
	items[0].Subject = "MODIFIED"
	if tm.Get("1").Subject != "MODIFIED" {
		t.Error("浅拷贝：修改指针内容应影响原始数据")
	}

	// 但替换/追加切片元素不影响原始切片
	items = append(items, &TodoItem{ID: "99", Subject: "C"})
	if len(tm.GetItems()) != 2 {
		t.Error("向副本追加元素不应影响原始切片")
	}
}

func TestTodoManager_Clear(t *testing.T) {
	tm := NewTodoManager()
	tm.Create("A", "")
	tm.Create("B", "")

	tm.Clear()

	if len(tm.GetItems()) != 0 {
		t.Error("Clear 后应为空")
	}

	// Clear 后 ID 重置
	item := tm.Create("C", "")
	if item.ID != "1" {
		t.Errorf("Clear 后 ID 应重置为 '1', 得到 '%s'", item.ID)
	}
}

func TestTodoManager_String(t *testing.T) {
	tm := NewTodoManager()
	if tm.String() != "No todo items." {
		t.Errorf("空清单应返回 'No todo items.', 得到 '%s'", tm.String())
	}

	tm.Create("任务一", TodoPending)
	tm.Create("任务二", TodoCompleted)

	s := tm.String()
	if s == "" || s == "No todo items." {
		t.Error("非空清单不应返回空或 'No todo items.'")
	}
}

// --- TodoWriteTool ---

func TestNewTodoWriteTool(t *testing.T) {
	tm := NewTodoManager()
	tw, err := NewTodoWriteTool(tm)
	if err != nil {
		t.Fatalf("创建 TodoWrite 工具失败: %v", err)
	}

	input := `{"todos":[{"subject":"分析代码","status":"pending"},{"subject":"写测试"}]}`
	result, err := tw.InvokableRun(context.Background(), input)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	if result == "" {
		t.Error("结果不应为空")
	}

	items := tm.GetItems()
	if len(items) != 2 {
		t.Errorf("期望 2 项, 得到 %d", len(items))
	}
	if items[0].Subject != "分析代码" {
		t.Errorf("期望 '分析代码', 得到 '%s'", items[0].Subject)
	}
	if items[1].Status != TodoPending {
		t.Errorf("第二项默认状态应为 'pending', 得到 '%s'", items[1].Status)
	}
}

func TestNewTodoWriteTool_覆盖(t *testing.T) {
	tm := NewTodoManager()
	tw, _ := NewTodoWriteTool(tm)

	// 第一次写入
	tw.InvokableRun(context.Background(), `{"todos":[{"subject":"A"}]}`)
	if len(tm.GetItems()) != 1 {
		t.Error("第一次应有 1 项")
	}

	// 第二次写入应覆盖
	tw.InvokableRun(context.Background(), `{"todos":[{"subject":"B"},{"subject":"C"}]}`)
	if len(tm.GetItems()) != 2 {
		t.Errorf("第二次应覆盖为 2 项, 得到 %d", len(tm.GetItems()))
	}
	if tm.Get("1").Subject != "B" {
		t.Errorf("覆盖后第一项应为 'B', 得到 '%s'", tm.Get("1").Subject)
	}
}

// --- TodoUpdateTool ---

func TestNewTodoUpdateTool(t *testing.T) {
	tm := NewTodoManager()
	tm.Create("任务一", "")

	tu, err := NewTodoUpdateTool(tm)
	if err != nil {
		t.Fatalf("创建 TodoUpdate 工具失败: %v", err)
	}

	// 更新状态
	result, err := tu.InvokableRun(context.Background(), `{"id":"1","status":"in_progress"}`)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if result == "" {
		t.Error("结果不应为空")
	}
	if tm.Get("1").Status != TodoInProgress {
		t.Errorf("期望 'in_progress', 得到 '%s'", tm.Get("1").Status)
	}

	// 更新标题
	result, _ = tu.InvokableRun(context.Background(), `{"id":"1","subject":"新标题"}`)
	if tm.Get("1").Subject != "新标题" {
		t.Errorf("期望 '新标题', 得到 '%s'", tm.Get("1").Subject)
	}
}

func TestNewTodoUpdateTool_不存在的ID(t *testing.T) {
	tm := NewTodoManager()
	tu, _ := NewTodoUpdateTool(tm)

	result, err := tu.InvokableRun(context.Background(), `{"id":"999","status":"completed"}`)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if result == "" {
		t.Error("结果不应为空")
	}
}
