-- Seed data for regression testing
-- Based on known issues from BUG-001 (Provider interface detection)

-- Known Issue 1: Provider interface false positive
INSERT INTO known_issues (
    issue_type, file_path, line_number, description,
    is_false_positive, verified_by, verified_at, verification_notes,
    related_bug_id
) VALUES (
    'false_positive',
    'internal/embed/provider.go',
    0,
    'Provider接口被报告为未使用，但TrigramProvider实现了它',
    1,
    'manual_review',
    '2026-06-16',
    '通过AST分析确认TrigramProvider实现了Provider接口',
    'BUG-001'
);

-- Known Issue 2: Real complexity issue
INSERT INTO known_issues (
    issue_type, file_path, line_number, description,
    is_false_positive, verified_by, verified_at, verification_notes
) VALUES (
    'true_positive',
    'cmd/cerberus/main.go',
    62,
    'initCmd函数圈复杂度14，超过阈值10',
    0,
    'automated_analysis',
    '2026-06-16',
    '通过AST分析验证，确实有过多嵌套逻辑'
);

-- Known Issue 3: Real file length issue
INSERT INTO known_issues (
    issue_type, file_path, line_number, description,
    is_false_positive, verified_by, verified_at, verification_notes
) VALUES (
    'true_positive',
    'cmd/cerberus/main.go',
    0,
    '文件507行，超过阈值150行',
    0,
    'automated_analysis',
    '2026-06-16',
    '统计代码行数验证，确实过长'
);

-- Regression Test 1: BUG-001 - Provider interface detection (positive)
INSERT INTO regression_tests (
    name, bug_id, category, test_type,
    description, file_path, interface_name,
    expected_result, notes
) VALUES (
    'BUG-001-Provider接口检测',
    'BUG-001',
    'abstraction',
    'positive',
    '应该检测到TrigramProvider实现了Provider接口',
    'internal/embed/trigram.go',
    'Provider',
    'detected_implementation',
    '当前只检查嵌入，需要实现方法签名匹配'
);

-- Regression Test 2: BUG-001 - Negative test
INSERT INTO regression_tests (
    name, bug_id, category, test_type,
    description, expected_result, notes
) VALUES (
    'BUG-001-不存在接口不应检测',
    'BUG-001',
    'abstraction',
    'negative',
    '不存在的接口不应该报告有实现',
    'zero_implementations',
    '验证不会有误报'
);

-- Regression Test 3: Complexity - main.go function complexity
INSERT INTO regression_tests (
    name, category, test_type,
    description, file_path,
    expected_result, notes
) VALUES (
    '检测-initCmd高复杂度',
    'complexity',
    'positive',
    '应该检测到initCmd函数的圈复杂度问题',
    'cmd/cerberus/main.go',
    'detected_issue',
    'initCmd函数圈复杂度14，超过阈值10'
);

-- Regression Test 4: File length - main.go
INSERT INTO regression_tests (
    name, category, test_type,
    description, file_path,
    expected_result, notes
) VALUES (
    '检测main.go文件过长',
    'complexity',
    'positive',
    '应该检测到main.go文件过长',
    'cmd/cerberus/main.go',
    'detected_issue',
    '文件507行，超过阈值150行'
);

-- Bug Record 1
INSERT INTO bug_tracker (
    bug_id, title, description, severity, category,
    affected_component, status, root_cause
) VALUES (
    'BUG-001',
    'Provider接口检测不准确',
    '架构分析器没有正确检测到TrigramProvider实现了Provider接口，导致误报为未使用接口',
    'major',
    'accuracy',
    'internal/architecture/abstraction.go',
    'open',
    '只检查了嵌入关系，没有实现方法签名匹配'
);

-- Initial accuracy report (baseline)
INSERT INTO accuracy_reports (
    run_id, timestamp, project_path,
    total_issues, true_positives, false_positives, true_negatives,
    overall_accuracy, complexity_accuracy, abstraction_accuracy, solid_accuracy,
    analyzer_version
) VALUES (
    'BASELINE-2026-06-16',
    '2026-06-16',
    '/home/mason/Documents/code_projects/private/cerberus',
    4,  -- Total issues
    2,  -- True positives (complexity issues in main.go)
    1,  -- False positives (Provider interface)
    0,  -- True negatives
    0.50, -- Overall accuracy (TP+TN)/Total = 2/4 = 50%
    1.00, -- Complexity accuracy: 2/2 = 100%
    0.00, -- Abstraction accuracy: 0/1 = 0% (false positive)
    NULL, -- SOLID accuracy: no tests yet
    'dev'
);
