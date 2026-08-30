// want: feedback-post-test-covers-bearer-cases
import 'package:test/test.dart';

void main() {
  // Regression: the present-token assertion is intact but the null-token
  // no-header assertion was dropped, so the guard is half-blind.
  test('attaches bearer when token present', () {
    expect(header, 'Bearer staff-token-321');
  });
}
