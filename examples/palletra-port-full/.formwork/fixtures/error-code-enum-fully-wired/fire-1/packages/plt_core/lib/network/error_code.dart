import '../../../../shared/generated/dart/lib/palletra/api/v1/error_code.pbenum.dart';

/// Every wire string the backend can send must resolve through this map;
/// do not scatter a second lookup table anywhere else in the frontend.
const Map<String, ErrorCode> _protoToErrorCode = <String, ErrorCode>{
  'stale_revision': ErrorCode.ERROR_CODE_STALE_REVISION,
  'module_disabled': ErrorCode.ERROR_CODE_MODULE_DISABLED,
};

ErrorCode errorCodeFromTransport(String wire) {
  return _protoToErrorCode[wire] ?? ErrorCode.ERROR_CODE_UNSPECIFIED;
}
