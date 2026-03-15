#include "trace.h"
#include "trace_options.h"

int trace_value(void) {
#if defined(HAVE_UNISTD_H) && defined(TRACE_FEATURE_API)
	return 11;
#elif defined(HAVE_UNISTD_H)
	return 7;
#else
	return 3;
#endif
}
