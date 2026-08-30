// Clean: server-provided labels, generic codes only, variable comparison,
// and a map key that must NOT trip either sub-rule.
String label(o) => o.label; // from ListTaskViewsResponse.dock_status_options

bool matches(job, String status) => job.dockStatus == status; // variable RHS

const genericCodes = ['in_progress', 'blocked', 'done'];

final payload = {'dock_status': 'in_progress'};
