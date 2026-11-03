#!/usr/bin/env python3
"""Prepare, bind and verify immutable protected-review image admission."""
import argparse
import json
from pathlib import Path
import subprocess
import sys

import image_admission as admission


class PrivateParser(argparse.ArgumentParser):
    def error(self, message):
        self.exit(2, 'BLOCKED: invalid admission arguments; consult --help.\n')


def environment_config(path, context):
    root = Path(__file__).resolve().parents[2]
    path = Path(path).absolute()
    admission.require(path.is_relative_to(root), 'setup record must be checked out from verifier source')
    raw = admission.read_bytes(path, admission.REQUEST_LIMIT)
    relative = path.relative_to(root).as_posix()
    reference = context['verifier_sha'] + ':' + relative
    size = subprocess.run(['git', '-C', str(root), 'cat-file', '-s', reference],
                          stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=10)
    admission.require(size.returncode == 0 and size.stdout.strip().isdigit()
                      and 0 < int(size.stdout.strip()) <= admission.REQUEST_LIMIT, 'tracked setup record unavailable')
    source = subprocess.run(['git', '-C', str(root), 'show', reference],
                            stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=10)
    admission.require(source.returncode == 0 and source.stdout == raw, 'setup record differs from verifier source')
    return admission.parse(raw, admission.REQUEST_LIMIT)


def main():
    parser = PrivateParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True, parser_class=PrivateParser)
    prepare = commands.add_parser('prepare')
    prepare.add_argument('--scan-dir', required=True, type=Path)
    prepare.add_argument('--privacy-receipt', required=True, type=Path)
    prepare.add_argument('--output-dir', required=True, type=Path)
    prepare.add_argument('--request-file', type=Path)
    for name in ('bind', 'approve', 'final'):
        child = commands.add_parser(name)
        child.add_argument('--bundle-dir', required=True, type=Path)
        child.add_argument('--artifact-id', required=True)
        child.add_argument('--artifact-digest', required=True)
        if name == 'approve':
            child.add_argument('--environment-config', type=Path, required=True)
            child.add_argument('--output-file', type=Path, required=True)
        elif name == 'final':
            child.add_argument('--prepare-result', required=True)
            child.add_argument('--admission-result', required=True)
            child.add_argument('--admission-file', type=Path)
            child.add_argument('--admission-artifact-id')
            child.add_argument('--admission-artifact-digest')
            child.add_argument('--environment-config', type=Path)
    args = parser.parse_args()
    context = admission.runner_context()
    if args.command == 'prepare':
        request = admission.parse(admission.read_bytes(args.request_file, admission.REQUEST_LIMIT),
                                  admission.REQUEST_LIMIT) if args.request_file else None
        if args.request_file is not None:
            admission.keys(request, {'format', 'requests'})
        result = admission.prepare(args.scan_dir, args.privacy_receipt, args.output_dir, request, context=context)
    else:
        api = admission.GitHubAPI()
        if args.command == 'bind':
            result = admission.bind(args.bundle_dir, args.artifact_id, args.artifact_digest, context=context, api=api)
        elif args.command == 'approve':
            result = admission.approve(args.bundle_dir, args.artifact_id, args.artifact_digest,
                                       environment_config(args.environment_config, context), context=context, api=api)
            admission.regular_directory(args.output_file.absolute().parent)
            admission.write_file(args.output_file, result)
        else:
            result = admission.final_result(args.bundle_dir, args.artifact_id, args.artifact_digest,
                args.prepare_result, args.admission_result, context=context, api=api,
                admission_file=args.admission_file, admission_artifact_id=args.admission_artifact_id,
                admission_artifact_digest=args.admission_artifact_digest,
                environment_config=environment_config(args.environment_config, context) if args.environment_config else None)
    print(admission.canonical(result).decode())
    return 0


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (admission.AdmissionError, OSError, ValueError, TypeError, KeyError, AttributeError,
            OverflowError, RecursionError, subprocess.SubprocessError):
        print('BLOCKED: incomplete or invalid protected-review evidence.', file=sys.stderr)
        raise SystemExit(1)
