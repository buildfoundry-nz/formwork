import 'package:test/test.dart';

void main() {
  test('attaches bearer when token present', () {
    expect(header, 'Bearer staff-token-321');
  });

  test('omits header when token null', () {
    expect([reqBearer, reqScheme], [null, null]);
  });
}
