#!/usr/bin/env python3
"""
Telegram uploader using pyrogram with support for large files (>2GB) via 7zip splitting.
Communicates with Go backend via JSON on stdin/stdout.
"""

import sys
import json
import os
import asyncio
import shutil
import subprocess
from pathlib import Path
from typing import Optional, Callable
import logging

try:
    from pyrogram import Client
    from pyrogram.types import Document
    from pyrogram.errors import RPCError
except ImportError:
    print("ERROR: pyrogram not installed. Run: pip install pyrogram")
    sys.exit(1)

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='[%(asctime)s] %(levelname)s: %(message)s'
)
logger = logging.getLogger(__name__)

# Constants
MAX_FILESIZE_SINGLE = 2 * 1024 * 1024 * 1024  # 2GB - split anything larger
CHUNK_SIZE = 512 * 1024  # 512KB chunks
SESSION_NAME = "tordown_session"
SESSION_DIR = Path.home() / ".tordown"

class TelegramUploader:
    def __init__(self, api_id: int, api_hash: str, phone: str, session_dir: Optional[Path] = None):
        """Initialize Telegram uploader with pyrogram."""
        self.api_id = api_id
        self.api_hash = api_hash
        self.phone = phone
        self.session_dir = session_dir or SESSION_DIR
        self.session_dir.mkdir(parents=True, exist_ok=True)
        
        self.client = Client(
            name=SESSION_NAME,
            api_id=api_id,
            api_hash=api_hash,
            phone_number=phone,
            workdir=str(self.session_dir),
            in_memory=False
        )
    
    async def ensure_authenticated(self) -> bool:
        """Ensure client is authenticated and connected."""
        try:
            async with self.client:
                me = await self.client.get_me()
                logger.info(f"✓ Authenticated as {me.first_name} ({me.phone_number})")
                return True
        except Exception as e:
            logger.error(f"Authentication failed: {e}")
            return False
    
    async def upload_file(self, file_path: str, on_progress: Optional[Callable] = None) -> bool:
        """
        Upload file to Telegram Saved Messages.
        Splits files >2GB into parts before uploading.
        
        Args:
            file_path: Path to file to upload
            on_progress: Optional callback(current_bytes, total_bytes)
        
        Returns:
            True if successful, False otherwise
        """
        try:
            file_path = Path(file_path)
            if not file_path.exists():
                logger.error(f"File not found: {file_path}")
                return False
            
            file_size = file_path.stat().st_size
            logger.info(f"Starting upload: {file_path.name} ({self._format_size(file_size)})")
            
            # Check if file needs splitting
            if file_size > MAX_FILESIZE_SINGLE:
                logger.info(f"File exceeds 2GB, splitting with 7zip...")
                parts = await self._split_file_7zip(file_path)
                if not parts:
                    logger.error("File splitting failed")
                    return False
                
                # Upload each part
                async with self.client:
                    total_uploaded = 0
                    for i, part_path in enumerate(parts, 1):
                        part_size = Path(part_path).stat().st_size
                        logger.info(f"Uploading part {i}/{len(parts)}: {Path(part_path).name} ({self._format_size(part_size)})")
                        
                        # Create progress callback for this part
                        def part_progress(current, total):
                            progress_pct = (total_uploaded + current) / file_size * 100
                            logger.info(f"  Part {i}/{len(parts)}: {progress_pct:.1f}% ({self._format_size(total_uploaded + current)}/{self._format_size(file_size)})")
                            if on_progress:
                                on_progress(total_uploaded + current, file_size)
                        
                        # Upload part
                        if not await self._upload_to_saved_messages(part_path, part_progress):
                            logger.error(f"Failed to upload part {i}/{len(parts)}")
                            # Cleanup split files
                            for p in parts:
                                try:
                                    Path(p).unlink()
                                except:
                                    pass
                            return False
                        
                        total_uploaded += part_size
                        logger.info(f"✓ Part {i}/{len(parts)} uploaded successfully")
                    
                    # Cleanup split files
                    for part_path in parts:
                        try:
                            Path(part_path).unlink()
                            logger.info(f"Cleaned up: {Path(part_path).name}")
                        except Exception as e:
                            logger.warning(f"Could not cleanup {part_path}: {e}")
            else:
                # Upload single file
                async with self.client:
                    if not await self._upload_to_saved_messages(str(file_path), on_progress):
                        logger.error("Upload failed")
                        return False
            
            logger.info(f"✓ Upload complete: {file_path.name}")
            return True
            
        except Exception as e:
            logger.error(f"Upload error: {e}", exc_info=True)
            return False
    
    async def _upload_to_saved_messages(self, file_path: str, on_progress: Optional[Callable] = None) -> bool:
        """Upload file to Saved Messages (InputPeerSelf)."""
        try:
            file_path = Path(file_path)
            caption = f"📁 {file_path.name}"
            
            await self.client.send_document(
                chat_id="me",  # Saved Messages
                document=str(file_path),
                caption=caption,
                progress=on_progress
            )
            return True
        except Exception as e:
            logger.error(f"Failed to upload to Saved Messages: {e}")
            return False
    
    async def _split_file_7zip(self, file_path: Path) -> list[str]:
        """
        Split file using 7zip into 2GB parts.
        Returns list of part file paths.
        """
        try:
            # Check if 7zip is installed
            if shutil.which("7z") is None:
                logger.error("7zip not found. Install: apt-get install p7zip-full (Linux) or choco install 7zip (Windows)")
                return []
            
            split_dir = file_path.parent / f"{file_path.stem}_split"
            split_dir.mkdir(exist_ok=True)
            
            # Use 7zip to split file into 2GB parts
            cmd = [
                "7z",
                "a",
                f"-v2g",  # Split into 2GB volumes
                str(split_dir / f"{file_path.stem}.7z"),
                str(file_path)
            ]
            
            logger.info(f"Running: {' '.join(cmd)}")
            result = subprocess.run(cmd, capture_output=True, text=True)
            
            if result.returncode != 0:
                logger.error(f"7zip failed: {result.stderr}")
                return []
            
            # Collect all part files
            parts = sorted(split_dir.glob(f"{file_path.stem}.7z.*"))
            logger.info(f"Created {len(parts)} parts: {[p.name for p in parts]}")
            
            return [str(p) for p in parts]
            
        except Exception as e:
            logger.error(f"File splitting error: {e}", exc_info=True)
            return []
    
    @staticmethod
    def _format_size(bytes: int) -> str:
        """Format bytes to human-readable size."""
        for unit in ['B', 'KB', 'MB', 'GB', 'TB']:
            if bytes < 1024:
                return f"{bytes:.1f}{unit}"
            bytes /= 1024
        return f"{bytes:.1f}PB"


async def main():
    """Main entry point - reads JSON from stdin, uploads file, returns JSON result."""
    try:
        # Read JSON input from Go backend
        input_json = sys.stdin.read()
        request = json.loads(input_json)
        
        api_id = request.get("api_id", 0)
        api_hash = request.get("api_hash", "")
        phone = request.get("phone", "")
        file_path = request.get("file_path", "")
        session_dir = request.get("session_dir")
        
        if not all([api_id, api_hash, file_path]):
            result = {
                "success": False,
                "error": "Missing required fields: api_id, api_hash, file_path"
            }
            print(json.dumps(result))
            return
        
        # Phone is optional if we have a session
        if not phone:
            logger.info("No phone provided, assuming existing session")
        
        # Create uploader
        uploader = TelegramUploader(
            api_id=api_id,
            api_hash=api_hash,
            phone=phone,
            session_dir=Path(session_dir) if session_dir else None
        )
        
        # Ensure authenticated
        if not await uploader.ensure_authenticated():
            result = {
                "success": False,
                "error": "Failed to authenticate with Telegram"
            }
            print(json.dumps(result))
            return
        
        # Upload file
        def progress_callback(current: int, total: int):
            """Report progress back to Go backend via stderr."""
            pct = (current / total * 100) if total > 0 else 0
            sys.stderr.write(f"[PROGRESS] {pct:.1f}% ({uploader._format_size(current)}/{uploader._format_size(total)})\n")
            sys.stderr.flush()
        
        success = await uploader.upload_file(file_path, on_progress=progress_callback)
        
        result = {
            "success": success,
            "file": Path(file_path).name,
            "message": "Upload completed successfully" if success else "Upload failed"
        }
        print(json.dumps(result))
        
    except json.JSONDecodeError as e:
        result = {
            "success": False,
            "error": f"Invalid JSON input: {e}"
        }
        print(json.dumps(result))
    except Exception as e:
        result = {
            "success": False,
            "error": f"Unexpected error: {e}"
        }
        print(json.dumps(result))


if __name__ == "__main__":
    asyncio.run(main())
