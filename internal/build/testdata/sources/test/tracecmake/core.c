#include "trace.h"
#include "trace_config.h"

int trace_value(void) {
#ifdef HAVE_UNISTD_H
	return 7;
#else
	return 3;
#endif
}
