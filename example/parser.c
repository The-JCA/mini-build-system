#include "parser.h"

int parse_digit(char digit)
{
    if (digit < '0' || digit > '9') {
        return -1;
    }
    return digit - '0';
}
