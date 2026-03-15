cmake_minimum_required(VERSION 3.16)
project(tracecmake C)

include(CheckIncludeFile)
check_include_file("unistd.h" HAVE_UNISTD_H)

configure_file(trace_config.h.in trace_config.h @ONLY)

add_library(tracecore STATIC core.c)
target_include_directories(tracecore PUBLIC "${CMAKE_CURRENT_SOURCE_DIR}" "${CMAKE_CURRENT_BINARY_DIR}")

add_executable(tracecli cli.c)
target_link_libraries(tracecli PRIVATE tracecore)

install(TARGETS tracecore tracecli
  ARCHIVE DESTINATION lib
  LIBRARY DESTINATION lib
  RUNTIME DESTINATION bin)
install(FILES
  "${CMAKE_CURRENT_SOURCE_DIR}/trace.h"
  "${CMAKE_CURRENT_BINARY_DIR}/trace_config.h"
  DESTINATION include)
