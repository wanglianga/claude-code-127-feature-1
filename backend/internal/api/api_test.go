package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"skirental/internal/config"
	"skirental/internal/db"
	"skirental/internal/rules"
	"skirental/internal/seed"

	_ "github.com/lib/pq"
)

var (
	ts     *httptest.Server
	client = &http.Client{}
	testDB *sql.DB
)

const testDBName = "skirental_test"

func TestMain(m *testing.M) {
	if err := setup(); err != nil {
		fmt.Fprintln(os.Stderr, "集成测试初始化失败（需要本地 PostgreSQL）:", err)
		os.Exit(1)
	}
	code := m.Run()
	teardown()
	os.Exit(code)
}

func setup() error {
	// 从 DATABASE_URL 推导 postgres 维护库连接
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		raw = "postgres://postgres@127.0.0.1:5433/skirental?sslmode=disable"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	adminURL := *u
	adminURL.Path = "/postgres"
	adm, err := sql.Open("postgres", adminURL.String())
	if err != nil {
		return err
	}
	defer adm.Close()
	if err := adm.Ping(); err != nil {
		return fmt.Errorf("无法连接 PostgreSQL（跳过集成测试需先启动本地库）: %w", err)
	}
	adm.Exec(`DROP DATABASE IF EXISTS ` + testDBName + ` WITH (FORCE)`)
	if _, err := adm.Exec(`CREATE DATABASE ` + testDBName); err != nil {
		return err
	}

	target := *u
	target.Path = "/" + testDBName
	var err2 error
	testDB, err2 = db.Open(target.String())
	if err2 != nil {
		return err2
	}
	if err := db.Migrate(testDB); err != nil {
		return err
	}
	if err := seed.Run(testDB); err != nil {
		return err
	}

	cfg := config.Get()
	cfg.UploadDir = os.TempDir() + "/ski-test-uploads"
	os.MkdirAll(cfg.UploadDir, 0o755)
	handler := NewServer(testDB, cfg)
	ts = httptest.NewServer(handler)
	return nil
}

func teardown() {
	if ts != nil {
		ts.Close()
	}
}

// ---------- HTTP 辅助 ----------

func login(t *testing.T, phone string) string {
	t.Helper()
	var d struct {
		Token string `json:"token"`
	}
	req(t, "POST", "/auth/login", "", map[string]string{"phone": phone, "password": "ski123456"}, &d)
	if d.Token == "" {
		t.Fatal("登录未返回 token")
	}
	return d.Token
}

func req(t *testing.T, method, path, token string, body interface{}, out interface{}) (int, map[string]string) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	r, err := http.NewRequest(method, ts.URL+"/api"+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if out != nil && len(data) > 0 {
		_ = json.Unmarshal(data, out)
	}
	var errBody struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(data, &errBody)
	return resp.StatusCode, map[string]string{"error": errBody.Error, "raw": string(data)}
}

func mustOK(t *testing.T, code int, errMsg string) {
	t.Helper()
	if code < 200 || code >= 300 {
		t.Fatalf("期望 2xx，实际 %d：%s", code, errMsg)
	}
}

// ---------- 测试用例 ----------

func TestAuthAndProfile(t *testing.T) {
	// 错误密码
	code, _ := req(t, "POST", "/auth/login", "", map[string]string{"phone": "13900000001", "password": "wrong"}, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("错误密码应 401，实际 %d", code)
	}
	g := login(t, "13900000001")
	// 无 token 访问受保护接口
	if code, _ = req(t, "GET", "/rentals/mine", "", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("无 token 应 401，实际 %d", code)
	}
	var me map[string]interface{}
	code, _ = req(t, "GET", "/me", g, nil, &me)
	mustOK(t, code, "")
	if me["role"] != "guest" || me["height_cm"].(float64) != 170 {
		t.Fatalf("个人资料异常: %+v", me)
	}
	// 角色越权：游客不能访问财务
	code, _ = req(t, "GET", "/finance/summary", g, nil, nil)
	if code != http.StatusForbidden {
		t.Fatalf("游客访问财务应 403，实际 %d", code)
	}
}

func TestSuggestions(t *testing.T) {
	g := login(t, "13900000001") // 170/65/42/57 beginner
	var d struct {
		Suggestions []struct {
			EquipmentType string `json:"equipment_type"`
			Size          string `json:"size"`
		} `json:"suggestions"`
	}
	code, e := req(t, "POST", "/guest/suggestions", g, map[string]string{"session_type": "day"}, &d)
	mustOK(t, code, e["error"])
	got := map[string]string{}
	for _, s := range d.Suggestions {
		got[s.EquipmentType] = s.Size
	}
	if got["ski"] != "145cm" || got["boot"] != "EU42" || got["helmet"] != "M" || got["pole"] != "120cm" || got["protector"] != "L" {
		t.Fatalf("初学者建议尺码异常: %+v", got)
	}
}

// 全流程：下单 → 确认预约 → 扫码出库 → 教练确认 → 跨码换鞋 →
// 中途板刃损伤（自动换发+维修+保险立案）→ 日场归还结算 → 保险赔付冲抵押金
func TestFullRentalLifecycle(t *testing.T) {
	g := login(t, "13900000004") // 刘队长 180/88/45/60 advanced
	counter := login(t, "13800000003")
	coach := login(t, "13800000002")
	repair := login(t, "13800000004")
	ins := login(t, "13800000005")
	fin := login(t, "13800000006")
	admin := login(t, "13800000001")

	// 选一个有余量的日场取具槽
	var slots []struct {
		ID        int64 `json:"id"`
		Remaining int   `json:"remaining"`
	}
	req(t, "GET", "/slots?session=day", g, nil, &slots)
	var slotID int64
	for _, s := range slots {
		if s.Remaining >= 2 {
			slotID = s.ID
			break
		}
	}
	if slotID == 0 {
		t.Fatal("没有可用日场时段")
	}

	// 1. 下单（5 件含雪杖，安心险）
	var created struct {
		Rentals []struct {
			RentalID     int64   `json:"rental_id"`
			DepositTotal float64 `json:"deposit_total"`
			FeeTotal     float64 `json:"fee_total"`
			PremiumTotal float64 `json:"premium_total"`
		} `json:"rentals"`
	}
	code, e := req(t, "POST", "/rentals", g, map[string]interface{}{
		"session_type": "day", "slot_id": slotID, "insurance_product_id": 2,
		"include": map[string]bool{"pole": true, "protector": true},
	}, &created)
	mustOK(t, code, e["error"])
	if len(created.Rentals) != 1 {
		t.Fatalf("个人下单应生成 1 单，实际 %d", len(created.Rentals))
	}
	rid := created.Rentals[0].RentalID
	if created.Rentals[0].PremiumTotal != 30 || created.Rentals[0].DepositTotal <= 0 {
		t.Fatalf("费用异常: %+v", created.Rentals[0])
	}

	// 2. 确认预约
	code, e = req(t, "POST", fmt.Sprintf("/rentals/%d/confirm", rid), g, map[string]string{}, nil)
	mustOK(t, code, e["error"])

	// 3. 柜台整单出库
	var out struct {
		DepositHold float64 `json:"deposit_hold"`
	}
	code, e = req(t, "POST", "/counter/checkout", counter,
		map[string]interface{}{"rental_id": rid}, &out)
	mustOK(t, code, e["error"])
	if out.DepositHold != created.Rentals[0].DepositTotal {
		t.Fatalf("冻结押金 %.2f 与押金合计 %.2f 不一致", out.DepositHold, created.Rentals[0].DepositTotal)
	}

	// 4. 扫码：雪具应显示在刘队长手中
	var detail struct {
		Items []struct {
			ID            int64   `json:"id"`
			EquipmentType string  `json:"equipment_type"`
			QRCode        string  `json:"qr_code"`
			FitStatus     string  `json:"fit_status"`
			Deposit       float64 `json:"deposit"`
		} `json:"items"`
	}
	req(t, "GET", fmt.Sprintf("/rentals/%d", rid), g, nil, &detail)
	if len(detail.Items) != 5 {
		t.Fatalf("应有 5 件雪具，实际 %d", len(detail.Items))
	}
	var bootID, helmetID, skiID int64
	var skiQR string
	for _, i := range detail.Items {
		var scanned struct {
			HolderName string `json:"holder_name"`
		}
		code, e = req(t, "POST", "/counter/scan", counter, map[string]string{"qr_code": i.QRCode}, &scanned)
		mustOK(t, code, e["error"])
		if !strings.Contains(scanned.HolderName, "刘") {
			t.Fatalf("扫码持有人应为刘队长，实际 %q", scanned.HolderName)
		}
		switch i.EquipmentType {
		case "boot":
			bootID = i.ID
		case "helmet":
			helmetID = i.ID
		case "ski":
			skiID = i.ID
			skiQR = i.QRCode
		}
	}

	// 5. 教练课前确认：雪鞋合脚通过；头盔标不合规 -> 安全提醒
	var fit struct {
		FitStatus string `json:"fit_status"`
	}
	code, e = req(t, "POST", "/coach/fit-check", coach,
		map[string]interface{}{"rental_item_id": bootID, "fit_status": "ok"}, &fit)
	mustOK(t, code, e["error"])
	if fit.FitStatus != "ok" {
		t.Fatal("雪鞋确认应通过")
	}
	req(t, "POST", "/coach/fit-check", coach,
		map[string]interface{}{"rental_item_id": helmetID, "fit_status": "issue", "note": "头盔偏松"}, nil)
	var rd struct {
		SafetyReminders []struct {
			Message string `json:"message"`
		} `json:"safety_reminders"`
	}
	req(t, "GET", fmt.Sprintf("/rentals/%d", rid), g, nil, &rd)
	reminded := false
	for _, s := range rd.SafetyReminders {
		if strings.Contains(s.Message, "头盔") {
			reminded = true
		}
	}
	if !reminded {
		t.Fatal("不合规后应收到安全提醒")
	}

	// 6. 换码：雪鞋 EU45 -> EU45.5
	var ex struct {
		ExchangeID int64 `json:"exchange_id"`
	}
	code, e = req(t, "POST", fmt.Sprintf("/rentals/%d/exchange-request", rid), g,
		map[string]interface{}{"rental_item_id": bootID, "reason": "挤脚", "desired_size": "EU45.5"}, &ex)
	mustOK(t, code, e["error"])
	var proc struct {
		NewSize string `json:"new_size"`
	}
	code, e = req(t, "POST", fmt.Sprintf("/counter/exchanges/%d/process", ex.ExchangeID), counter,
		map[string]interface{}{"approve": true}, &proc)
	mustOK(t, code, e["error"])
	if proc.NewSize != "EU45.5" {
		t.Fatalf("换发尺码应为 EU45.5，实际 %q", proc.NewSize)
	}

	// 7. 游客中途上报雪板板刃损伤，柜台评定
	var dmg struct {
		DamageID int64 `json:"damage_id"`
	}
	code, e = req(t, "POST", fmt.Sprintf("/rentals/%d/damage", rid), g,
		map[string]interface{}{"rental_item_id": skiID, "damage_type": "edge_damage",
			"severity": "medium", "description": "测试板刃缺口"}, &dmg)
	mustOK(t, code, e["error"])
	var assessed struct {
		DeductionHint float64 `json:"deduction_hint"`
	}
	code, e = req(t, "POST", fmt.Sprintf("/counter/damages/%d/assess", dmg.DamageID), counter,
		map[string]interface{}{"severity": "medium"}, &assessed)
	mustOK(t, code, e["error"])
	// 雪板押金 1500，板刃 35% = 525
	if assessed.DeductionHint != 525 {
		t.Fatalf("板刃损伤扣减应为 525，实际 %.2f", assessed.DeductionHint)
	}
	// 原雪板应进入维修，工单存在
	var orders []struct {
		Issue string `json:"issue"`
	}
	req(t, "GET", "/repair/orders", repair, nil, &orders)
	foundRO := false
	for _, o := range orders {
		if strings.Contains(o.Issue, "板刃") {
			foundRO = true
		}
	}
	if !foundRO {
		t.Fatal("应生成板刃维修工单")
	}
	// 自动换发的新雪板仍可扫码，且当前雪具不是原来那件
	var d2 struct {
		Items []struct {
			EquipmentType string `json:"equipment_type"`
			QRCode        string `json:"qr_code"`
			Status        string `json:"status"`
		} `json:"items"`
	}
	req(t, "GET", fmt.Sprintf("/rentals/%d", rid), g, nil, &d2)
	var newSkiQR string
	for _, i := range d2.Items {
		if i.EquipmentType == "ski" {
			newSkiQR = i.QRCode
			if i.Status != "out" {
				t.Fatalf("换发的雪板应处于 out，实际 %s", i.Status)
			}
		}
	}
	if newSkiQR == skiQR {
		t.Fatal("换发后雪板二维码应变化")
	}

	// 8. 排队日场归还，逐件完好验具后结算
	code, e = req(t, "POST", fmt.Sprintf("/rentals/%d/queue-return", rid), g,
		map[string]interface{}{"night": false}, nil)
	mustOK(t, code, e["error"])
	for {
		var cur struct {
			Items []struct {
				ID     int64  `json:"id"`
				Status string `json:"status"`
			} `json:"items"`
		}
		req(t, "GET", fmt.Sprintf("/rentals/%d", rid), g, nil, &cur)
		next := int64(0)
		for _, x := range cur.Items {
			if x.Status == "out" {
				next = x.ID
				break
			}
		}
		if next == 0 {
			break
		}
		code, e = req(t, "POST", fmt.Sprintf("/counter/returns/%d/inspect", rid), counter,
			map[string]interface{}{"rental_item_id": next, "result": "ok"}, nil)
		mustOK(t, code, e["error"])
	}
	var finish struct {
		Deducted float64 `json:"deducted"`
		Refund   float64 `json:"refund"`
	}
	code, e = req(t, "POST", fmt.Sprintf("/counter/returns/%d/finish", rid), counter,
		map[string]string{}, &finish)
	mustOK(t, code, e["error"])
	if finish.Deducted != 525 {
		t.Fatalf("归还时扣减应为 525，实际 %.2f", finish.Deducted)
	}
	if finish.Refund != created.Rentals[0].DepositTotal-525 {
		t.Fatalf("保险赔付前退还金额应为押金-525，实际 %.2f", finish.Refund)
	}

	// 9. 保险审核 → 核定（525-50 免赔=475）→ 赔付冲抵
	var claims []struct {
		ID       int64  `json:"id"`
		Status   string `json:"status"`
		RentalID int64  `json:"rental_id"`
	}
	req(t, "GET", "/insurance/claims", ins, nil, &claims)
	var claimID int64
	for _, c := range claims {
		if c.Status == "filed" && c.RentalID == rid {
			claimID = c.ID
		}
	}
	if claimID == 0 {
		t.Fatal("本单应有 filed 状态的理赔单")
	}
	req(t, "POST", fmt.Sprintf("/insurance/claims/%d/review", claimID), ins,
		map[string]interface{}{"status": "under_review", "note": "集成测试审核"}, nil)
	req(t, "POST", fmt.Sprintf("/insurance/claims/%d/review", claimID), ins,
		map[string]interface{}{"status": "approved", "note": "核定"}, nil)
	var pay struct {
		Payout float64 `json:"payout"`
	}
	code, e = req(t, "POST", fmt.Sprintf("/insurance/claims/%d/pay", claimID), ins,
		map[string]string{}, &pay)
	mustOK(t, code, e["error"])
	if pay.Payout != 475 {
		t.Fatalf("赔付应为 475（525-50 免赔），实际 %.2f", pay.Payout)
	}
	// 赔付后应有保险冲抵流水
	var ledger []struct {
		Type   string  `json:"type"`
		Amount float64 `json:"amount"`
	}
	req(t, "GET", "/finance/ledger", fin, nil, &ledger)
	var offset float64
	for _, l := range ledger {
		if l.Type == "insurance_offset" {
			offset += l.Amount
		}
	}
	if offset != 475 {
		t.Fatalf("保险冲抵流水应为 475，实际 %.2f", offset)
	}

	// 10. 反馈
	code, e = req(t, "POST", fmt.Sprintf("/rentals/%d/feedback", rid), g,
		map[string]interface{}{"rating": 5, "text": "集成测试好评"}, nil)
	mustOK(t, code, e["error"])

	// 11. 管理分析接口可用且安全提醒名单包含该游客
	var analytics map[string]json.RawMessage
	code, e = req(t, "GET", "/admin/analytics", admin, nil, &analytics)
	mustOK(t, code, e["error"])
	if _, ok := analytics["guest_safety"]; !ok {
		t.Fatal("分析结果缺少 guest_safety")
	}
}

func TestNightReturnSeparateQueue(t *testing.T) {
	// 团队成员-小钱 155/48/37 beginner，夜场
	g := login(t, "13900000005")
	counter := login(t, "13800000003")

	var slots []struct {
		ID        int64 `json:"id"`
		Remaining int   `json:"remaining"`
	}
	req(t, "GET", "/slots?session=night", g, nil, &slots)
	var slotID int64
	for _, s := range slots {
		if s.Remaining >= 1 {
			slotID = s.ID
			break
		}
	}
	var created struct {
		Rentals []struct {
			RentalID int64   `json:"rental_id"`
			FeeTotal float64 `json:"fee_total"`
		} `json:"rentals"`
	}
	code, e := req(t, "POST", "/rentals", g, map[string]interface{}{
		"session_type": "night", "slot_id": slotID,
		"include": map[string]bool{"protector": true},
	}, &created)
	mustOK(t, code, e["error"])
	rid := created.Rentals[0].RentalID
	// 夜场四件：板360+鞋120+盔72+护具96 = 648
	if created.Rentals[0].FeeTotal != 648 {
		t.Fatalf("夜场加价后租金应为 648，实际 %.2f", created.Rentals[0].FeeTotal)
	}
	req(t, "POST", fmt.Sprintf("/rentals/%d/confirm", rid), g, map[string]string{}, nil)
	req(t, "POST", "/counter/checkout", counter, map[string]interface{}{"rental_id": rid}, nil)
	req(t, "POST", fmt.Sprintf("/rentals/%d/queue-return", rid), g,
		map[string]interface{}{"night": true}, nil)

	var tickets []struct {
		QueueType string `json:"queue_type"`
		RentalID  int64  `json:"rental_id"`
	}
	req(t, "GET", "/counter/queue?type=return_night", counter, nil, &tickets)
	ok := false
	for _, tk := range tickets {
		if tk.RentalID == rid && tk.QueueType == "return_night" {
			ok = true
		}
	}
	if !ok {
		t.Fatal("夜场归还应进入独立的 return_night 队列")
	}
}

func TestTeamOrderAndPickupQueue(t *testing.T) {
	leader := login(t, "13900000004")
	var teams []struct {
		ID int64 `json:"id"`
	}
	req(t, "GET", "/teams/mine", leader, nil, &teams)
	if len(teams) != 1 || teams[0].ID != 1 {
		t.Fatalf("队长应属于 1 个团队，实际 %+v", teams)
	}
	var slots []struct {
		ID        int64 `json:"id"`
		Remaining int   `json:"remaining"`
	}
	req(t, "GET", "/slots?session=day", leader, nil, &slots)
	var slotID int64
	for _, s := range slots {
		if s.Remaining >= 3 {
			slotID = s.ID
			break
		}
	}
	if slotID == 0 {
		t.Skip("没有容量≥3的日场时段，跳过团队单测试")
	}
	var created struct {
		Rentals []struct {
			RentalID int64 `json:"rental_id"`
		} `json:"rentals"`
	}
	code, e := req(t, "POST", "/rentals", leader, map[string]interface{}{
		"team_id": 1, "session_type": "day", "slot_id": slotID,
		"include": map[string]bool{"protector": true},
	}, &created)
	mustOK(t, code, e["error"])
	if len(created.Rentals) != 3 {
		t.Fatalf("团队单应为 3 名成员各 1 单，实际 %d", len(created.Rentals))
	}
	// 队长确认本人订单 -> 取具队列出现团队优先票
	rid := created.Rentals[0].RentalID
	code, e = req(t, "POST", fmt.Sprintf("/rentals/%d/confirm", rid), leader,
		map[string]string{}, nil)
	mustOK(t, code, e["error"])
	var tickets []struct {
		RentalID int64 `json:"rental_id"`
		Priority int   `json:"priority"`
	}
	req(t, "GET", "/counter/queue?type=pickup", login(t, "13800000003"), nil, &tickets)
	found := false
	for _, tk := range tickets {
		if tk.RentalID == rid && tk.Priority == 1 {
			found = true
		}
	}
	if !found {
		t.Fatal("团队取具票应为高优先级")
	}
}

func TestMaintenanceGeneration(t *testing.T) {
	repair := login(t, "13800000004")
	var gen struct {
		Created int `json:"created"`
	}
	code, e := req(t, "POST", "/repair/maintenance/generate", repair, map[string]string{}, &gen)
	mustOK(t, code, e["error"])
	// 种子数据 + 全流程用例已产生板刃/内衬等损坏，至少应生成 1 条高损耗提醒
	if gen.Created < 1 {
		t.Fatalf("应生成至少 1 条保养提醒，实际 %d", gen.Created)
	}
}

// 课前尺码重配全链路：教练发现初学者雪鞋过松发起重配 → 柜台按库存推荐完成换发
// （课程顺延 + 游客补偿 + 教练排课同步）→ 教练重新确认安全动作与课程时间 → 游客端可见原因/收费/权益
func TestPreCourseRefit(t *testing.T) {
	g := login(t, "13900000006") // 团队成员-小孙 168/60/40 beginner
	counter := login(t, "13800000003")
	coach := login(t, "13800000002")

	// 直接造一堂"即将开始"的初学者课程（教练=陈教练），保证重配耗时会影响课程开始
	var courseID int64
	err := testDB.QueryRow(`INSERT INTO courses(name, coach_id, session_type, start_at, end_at, level, capacity)
		VALUES ('重配测试初学班', (SELECT id FROM users WHERE phone='13800000002'), 'day',
			now() + interval '5 minute', now() + interval '2 hour', 'beginner', 10)
		RETURNING id`).Scan(&courseID)
	if err != nil {
		t.Fatal(err)
	}
	var courseStart0 time.Time
	if err := testDB.QueryRow(`SELECT start_at FROM courses WHERE id=$1`, courseID).Scan(&courseStart0); err != nil {
		t.Fatal(err)
	}

	// 下单关联该课程（日场 5 件）
	var slots []struct {
		ID        int64 `json:"id"`
		Remaining int   `json:"remaining"`
	}
	req(t, "GET", "/slots?session=day", g, nil, &slots)
	var slotID int64
	for _, s := range slots {
		if s.Remaining >= 1 {
			slotID = s.ID
			break
		}
	}
	var created struct {
		Rentals []struct {
			RentalID int64   `json:"rental_id"`
			FeeTotal float64 `json:"fee_total"`
		} `json:"rentals"`
	}
	code, e := req(t, "POST", "/rentals", g, map[string]interface{}{
		"session_type": "day", "slot_id": slotID, "course_id": courseID,
		"include": map[string]bool{"pole": true, "protector": true},
	}, &created)
	mustOK(t, code, e["error"])
	rid := created.Rentals[0].RentalID
	fee0 := created.Rentals[0].FeeTotal
	req(t, "POST", fmt.Sprintf("/rentals/%d/confirm", rid), g, map[string]string{}, nil)
	req(t, "POST", "/counter/checkout", counter, map[string]interface{}{"rental_id": rid}, nil)

	// 找到雪鞋明细
	var detail struct {
		Items []struct {
			ID            int64  `json:"id"`
			EquipmentType string `json:"equipment_type"`
			FitStatus     string `json:"fit_status"`
		} `json:"items"`
	}
	req(t, "GET", fmt.Sprintf("/rentals/%d", rid), g, nil, &detail)
	var bootRI int64
	for _, i := range detail.Items {
		if i.EquipmentType == "boot" {
			bootRI = i.ID
		}
	}
	if bootRI == 0 {
		t.Fatal("未找到雪鞋明细")
	}

	// 1. 教练发起课前重配（小孙脚码 40，建议 EU40 -> 过松收半码 EU39.5）
	var refit struct {
		RefitID    int64  `json:"refit_id"`
		TicketNo   string `json:"ticket_no"`
		TargetSize string `json:"target_size"`
		EstMinutes int    `json:"est_minutes"`
	}
	code, e = req(t, "POST", "/coach/refits", coach, map[string]interface{}{
		"rental_item_id": bootRI, "reason": "初学者雪鞋过松，脚跟打滑影响安全",
	}, &refit)
	mustOK(t, code, e["error"])
	if refit.TargetSize != "EU39.5" {
		t.Fatalf("目标尺码应为 EU39.5，实际 %q", refit.TargetSize)
	}
	if refit.EstMinutes < 10 {
		t.Fatalf("预估耗时应 ≥10 分钟，实际 %d", refit.EstMinutes)
	}
	// 重复发起应冲突
	code, _ = req(t, "POST", "/coach/refits", coach, map[string]interface{}{
		"rental_item_id": bootRI, "reason": "重复",
	}, nil)
	if code != http.StatusConflict {
		t.Fatalf("重复发起应 409，实际 %d", code)
	}
	// 游客端同步显示：安全提醒 + 重配记录（含原因/预估耗时）
	var rd struct {
		SafetyReminders []struct {
			Message string `json:"message"`
		} `json:"safety_reminders"`
		Refits []struct {
			Status     string  `json:"status"`
			Reason     string  `json:"reason"`
			ChargeYuan float64 `json:"charge_yuan"`
		} `json:"refits"`
	}
	req(t, "GET", fmt.Sprintf("/rentals/%d", rid), g, nil, &rd)
	if len(rd.Refits) != 1 || rd.Refits[0].Status != "requested" {
		t.Fatalf("游客端应看到 requested 重配记录: %+v", rd.Refits)
	}
	reminded := false
	for _, s := range rd.SafetyReminders {
		if strings.Contains(s.Message, "重配") {
			reminded = true
		}
	}
	if !reminded {
		t.Fatal("游客应收到重配安全提醒")
	}

	// 2. 柜台重配队列：库存推荐应首推 EU39.5
	var pending []struct {
		RefitID         int64 `json:"refit_id"`
		Recommendations []struct {
			SizeLabel string `json:"size_label"`
			Stock     int    `json:"stock"`
		} `json:"recommendations"`
	}
	req(t, "GET", "/counter/refits/pending", counter, nil, &pending)
	var myRecs []struct {
		SizeLabel string `json:"size_label"`
		Stock     int    `json:"stock"`
	}
	found := false
	for _, p := range pending {
		if p.RefitID == refit.RefitID {
			found = true
			myRecs = p.Recommendations
		}
	}
	if !found {
		t.Fatal("柜台重配队列应包含该重配单")
	}
	if len(myRecs) == 0 || myRecs[0].SizeLabel != "EU39.5" || myRecs[0].Stock <= 0 {
		t.Fatalf("库存推荐应首推 EU39.5 且有货: %+v", myRecs)
	}

	// 3. 柜台完成重配：耗时 20 分钟（课程 5 分钟后开始 -> 顺延）
	var done struct {
		NewSize       string  `json:"new_size"`
		DelayMinutes  int     `json:"delay_minutes"`
		Compensate    float64 `json:"compensate_yuan"`
		ChargeYuan    float64 `json:"charge_yuan"`
		RightsImpact  string  `json:"rights_impact"`
		ActualMinutes int     `json:"actual_minutes"`
	}
	code, e = req(t, "POST", fmt.Sprintf("/counter/refits/%d/complete", refit.RefitID), counter,
		map[string]interface{}{"actual_minutes": 20}, &done)
	mustOK(t, code, e["error"])
	if done.NewSize != "EU39.5" {
		t.Fatalf("换发尺码应为 EU39.5，实际 %q", done.NewSize)
	}
	if done.DelayMinutes <= 0 {
		t.Fatalf("重配耗时 20 分钟应导致课程顺延，实际 %d", done.DelayMinutes)
	}
	if done.Compensate != rules.RefitCompensation(done.DelayMinutes) || done.Compensate <= 0 {
		t.Fatalf("补偿应按规则计算且 >0，实际 %.2f", done.Compensate)
	}
	if done.ChargeYuan != 0 {
		t.Fatalf("安全重配应免费，实际 %.2f", done.ChargeYuan)
	}
	if done.RightsImpact == "" {
		t.Fatal("应生成课程权益影响说明")
	}

	// 课程开始时间已顺延（教练排课同步调整）
	var courseStart1 time.Time
	if err := testDB.QueryRow(`SELECT start_at FROM courses WHERE id=$1`, courseID).Scan(&courseStart1); err != nil {
		t.Fatal(err)
	}
	if !courseStart1.After(courseStart0) {
		t.Fatalf("课程开始时间应顺延：%v -> %v", courseStart0, courseStart1)
	}
	// 租金已减免补偿金额，且有 compensation 流水
	var fee1 float64
	if err := testDB.QueryRow(`SELECT fee_total FROM rentals WHERE id=$1`, rid).Scan(&fee1); err != nil {
		t.Fatal(err)
	}
	if fee1 != fee0-done.Compensate {
		t.Fatalf("租金应减免 %.2f：%.2f -> %.2f", done.Compensate, fee0, fee1)
	}
	var compCnt int
	if err := testDB.QueryRow(`SELECT count(*) FROM deposit_transactions
		WHERE rental_id=$1 AND type='compensation' AND amount=$2`, rid, done.Compensate).Scan(&compCnt); err != nil {
		t.Fatal(err)
	}
	if compCnt != 1 {
		t.Fatal("应有一条 compensation 押金流水")
	}

	// 教练端课程列表同步显示权益变化与待重新确认
	var courses []struct {
		ID     int64 `json:"id"`
		Refits []struct {
			Status       string  `json:"status"`
			DelayMin     int     `json:"delay_minutes"`
			Compensate   float64 `json:"compensate_yuan"`
			RightsImpact string  `json:"rights_impact"`
		} `json:"refits"`
	}
	req(t, "GET", "/coach/courses", coach, nil, &courses)
	synced := false
	for _, co := range courses {
		if co.ID != courseID {
			continue
		}
		for _, rf := range co.Refits {
			if rf.Status == "done" && rf.DelayMin > 0 && rf.Compensate > 0 && rf.RightsImpact != "" {
				synced = true
			}
		}
	}
	if !synced {
		t.Fatal("教练端应同步显示重配权益变化（顺延/补偿/说明）")
	}

	// 4. 教练重新确认安全动作与课程时间
	code, _ = req(t, "POST", fmt.Sprintf("/coach/refits/%d/reconfirm", refit.RefitID), coach,
		map[string]interface{}{"safety_confirmed": true, "schedule_confirmed": false}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("未确认课程时间应 400，实际 %d", code)
	}
	var reconf struct {
		FitStatus string `json:"fit_status"`
	}
	code, e = req(t, "POST", fmt.Sprintf("/coach/refits/%d/reconfirm", refit.RefitID), coach,
		map[string]interface{}{"safety_confirmed": true, "schedule_confirmed": true,
			"note": "新鞋合脚，犁式刹车动作已重新确认"}, &reconf)
	mustOK(t, code, e["error"])
	if reconf.FitStatus != "ok" {
		t.Fatal("重新确认后雪鞋应为 ok")
	}

	// 5. 游客端最终状态：confirmed + 免费 + 权益说明 + 雪鞋 fit ok
	var final struct {
		Refits []struct {
			Status       string  `json:"status"`
			ChargeYuan   float64 `json:"charge_yuan"`
			Compensate   float64 `json:"compensate_yuan"`
			RightsImpact string  `json:"rights_impact"`
			DelayMin     int     `json:"delay_minutes"`
		} `json:"refits"`
		Items []struct {
			EquipmentType string `json:"equipment_type"`
			FitStatus     string `json:"fit_status"`
		} `json:"items"`
	}
	req(t, "GET", fmt.Sprintf("/rentals/%d", rid), g, nil, &final)
	if len(final.Refits) != 1 || final.Refits[0].Status != "confirmed" {
		t.Fatalf("重配应已闭环 confirmed: %+v", final.Refits)
	}
	if final.Refits[0].ChargeYuan != 0 || final.Refits[0].Compensate <= 0 ||
		final.Refits[0].DelayMin <= 0 || final.Refits[0].RightsImpact == "" {
		t.Fatalf("游客端权益信息不完整: %+v", final.Refits[0])
	}
	for _, i := range final.Items {
		if i.EquipmentType == "boot" && i.FitStatus != "ok" {
			t.Fatalf("重配确认后雪鞋课前状态应为 ok，实际 %s", i.FitStatus)
		}
	}
}
