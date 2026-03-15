cmake_minimum_required(VERSION 3.16)
project(traceoptions C)

include(CheckIncludeFile)
check_include_file("unistd.h" HAVE_UNISTD_H)

option(TRACE_FEATURE_API "toggle generated header consumed by compile" OFF)
option(TRACE_BUILD_CLI "build consumer executable" OFF)
option(TRACE_INSTALL_ALIAS "install extra delivery-only alias" OFF)

configure_file(trace_options.h.in trace_options.h @ONLY)

add_library(tracecore STATIC core.c)
target_include_directories(tracecore PUBLIC "${CMAKE_CURRENT_SOURCE_DIR}" "${CMAKE_CURRENT_BINARY_DIR}")

if(TRACE_BUILD_CLI)
  add_executable(tracecli cli.c)
  target_link_libraries(tracecli PRIVATE tracecore)
endif()

install(TARGETS tracecore
  ARCHIVE DESTINATION lib
  LIBRARY DESTINATION lib)
if(TRACE_BUILD_CLI)
  install(TARGETS tracecli RUNTIME DESTINATION bin)
endif()
install(FILES
  "${CMAKE_CURRENT_SOURCE_DIR}/trace.h"
  "${CMAKE_CURRENT_BINARY_DIR}/trace_options.h"
  DESTINATION include)
if(TRACE_INSTALL_ALIAS)
  install(FILES "${CMAKE_CURRENT_SOURCE_DIR}/trace.h" DESTINATION include RENAME trace_alias.h)
endif()
