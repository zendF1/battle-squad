# Walkthrough - Battle Squad Server Implementation

Chúng ta đã hoàn thành việc thiết kế và lập trình toàn bộ hệ thống Battle Squad game server bằng ngôn ngữ Go.

---

## 1. Kết Quả Đạt Được

### Phân rã 3 Process & Structuring
- Khởi tạo cấu trúc dự án: `cmd/api`, `cmd/game`, `cmd/migrate`, và `internal/` chia theo module Domain-Driven Design (DDD).
- Phân tách 2 Port chạy độc lập: REST API (`:8080` mặc định) và WebSockets (`:8081` mặc định) giúp cô lập lỗi giữa các luồng nghiệp vụ kinh tế và gameplay thời gian thực.

### Hạ tầng và Mẫu Thiết Kế Vận Hành (Patterns)
- **Config**: Tải cấu hình linh hoạt từ môi trường và file cấu hình YAML.
- **Database/Redis Connector**: Pool kết nối tự động tối ưu hóa kết nối, tích hợp ping kiểm tra sức khỏe.
- **Structured Logging (zerolog)**: Tự động đính kèm `correlation_id`, `player_id`, và `match_id` trong ngữ cảnh của log giúp việc tracing lỗi không cần restart toàn bộ hệ thống.
- **Panic Recovery**: Mỗi Match/Room goroutine có khối recover độc lập đảm bảo crash 1 phòng chơi không gây sập các phòng chơi khác.
- **Graceful Shutdown**: Hệ thống bắt tín hiệu hệ điều hành, dọn dẹp các luồng chạy trước khi tắt.
- **Circuit Breaker**: Fail-fast khi database hoặc Redis gặp sự cố.
- **Idempotency Keys & Economy Transactions**: Đảm bảo an toàn giao dịch mua sắm vật phẩm, đổi giftcode và nạp tiền (IAP) bằng cách kiểm tra trùng lặp thông qua Redis và lưu trữ sổ cái (Ledger) giao dịch.
- **3-Tier Health Check**: Hỗ trợ các endpoint `/healthz`, `/readyz`, và `/livez` cho giám sát hệ thống.

### Các Phân Hệ REST API (cmd/api)
1. **Auth & Profile**: Guest login, nâng cấp liên kết Apple/Google, quản lý xóa tài khoản (grace period 7 ngày).
2. **Inventory**: Quản lý số lượng vật phẩm khả dụng, tự động khấu trừ vật phẩm đang khóa (reserved) trong các phòng đấu.
3. **Shop**: Mua vật phẩm và nhân vật bằng Coin hoặc Gem, ràng buộc giới hạn lượt mua của mỗi người chơi.
4. **IAP Verify**: Xác thực giao dịch App Store/Google Play và cộng Gem tương ứng.
5. **Gift Code**: Đổi giftcode nhận Gem, Coin và Items, kiểm tra hạn sử dụng và số lượng tối đa.
6. **Missions**: Nhiệm vụ ngày và thành tích, theo dõi tiến trình và nhận thưởng.
7. **Ranks**: Bảng xếp hạng Elo theo mùa, phân tầng bậc rank (Bronze -> Master) và claim phần thưởng khi kết thúc mùa giải.
8. **Moderation**: Player report và cơ chế ban/unban tài khoản.
9. **App Policy**: Kiểm tra phiên bản bắt buộc (Force Update) và Remote Config cấu hình game từ xa.

### Phân Hệ Gameplay thời gian thực (cmd/game)
1. **WS Server & Handshake**: Upgrade HTTP -> WS, xác thực JWT token và lọc ban tài khoản khi kết nối.
2. **Room Actor Loop**: Mỗi phòng chơi chạy 1 goroutine xử lý tuần tự (single-threaded state machine) các thao tác: đổi team, chọn nhân vật/vật phẩm, ready và bắt đầu trận đấu.
3. **Match Physics Engine**: Mô phỏng quỹ đạo bay của đạn theo timestep 0.05s chịu ảnh hưởng của Trọng lực (Gravity) và Gió (Wind).
4. **Explosion, Collapse & Damage**: Tính toán bán kính nổ, giảm trừ sát thương theo chỉ số phòng thủ (Defense) của nhân vật, sập địa hình bitmap và gây sát thương rơi tự do (Fall Damage).
5. **Bot AI**: Bot tự động di chuyển, dùng Medkit cứu thương khi máu yếu và nhắm bắn mục tiêu gần nhất có kèm sai số góc bắn theo độ khó (Easy/Normal).

---

## 2. Kết Quả Verification

### Unit Tests
Chạy thành công 3 bộ kiểm thử logic vật lý và sát thương nổ trong `internal/game/match/`:
```bash
go test ./internal/game/match/... -v
```
Kết quả:
- `TestCalculateExplosionDamage`: **PASS** (tính chính xác độ giảm sát thương theo khoảng cách và chỉ số phòng thủ).
- `TestCalculateFallDamage`: **PASS** (tính chính xác sát thương rơi khi vượt ngưỡng 150px).
- `TestSimulateProjectile`: **PASS** (mô phỏng chính xác quỹ đạo đạn bay xiên, tích hợp gió và trọng lực).

### E2E Verification Script
Tự động dựng database/redis bằng Docker Compose, chạy script verify client (`verify_server.go` trong `scratch/`) thực hiện quy trình E2E:
1. Đọc App Policy `/app/version-policy` thành công.
2. Guest login `/auth/guest-login` thành công (tạo mới Account, Profile và mở khóa nhân vật Rookie + grant 3 item cơ bản).
3. Đọc thông tin Profile `/player/profile` thông qua Bearer JWT Token thành công.

Kết quả: **SERVER VERIFICATION PASSED SUCCESSFULLY**
